package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Event struct {
	SectionName string
	Host        string
	Timestamp   time.Time
	Method      string
	Resource    string
	Version     string
	Bytes       int
	Status      int
	Referer     string
	UserAgent   string
	IsError     bool
}

type Section struct {
	Name             string
	HitCount         int
	BytesTransferred int
	ErrorCount       int
}

type WebService struct {
	LogFilePath      string
	LogFile          *os.File
	LogFileFD        int
	ErrorLogFilePath string
	ErrorLogFile     *os.File
	BusiestSection   *Section
	HitCount         int
	BytesTransferred int
	ErrorCount       int
	HitLimit         int
	HighTraffic      bool
	LimitCrossed     time.Time
	Sections         map[string]*Section
}

var HTTP_METHODS = map[string]bool{
	"GET":     true,
	"HEAD":    true,
	"POST":    true,
	"PUT":     true,
	"DELETE":  true,
	"OPTIONS": true,
	"TRACE":   true,
	"CONNECT": true,
}

// Hack to prevent select from spinning up the CPU
const SELECT_SLEEP_MS = 35

// host, ident, auth, timestamp, request, status, bytes
var CLF_REGEXP = regexp.MustCompile(
	`(.*)\s(.*)\s(.*)\s\[(.*)\]\s"(.*)"\s(.*)\s(.*)`,
)

// host, ident, auth, timestamp, request, status, bytes, referer, user-agent
var ECLF_REGEXP = regexp.MustCompile(
	`(.*)\s(.*)\s(.*)\s\[(.*)\]\s"(.*)"\s(.*)\s(.*)\s"(.*)"\s"(.*)"`,
)

var WEB_SERVICE *WebService

func FD_SET(p *syscall.FdSet, i int) {
	p.Bits[i/64] |= 1 << uint(i) % 64
}

func FD_ISSET(p *syscall.FdSet, i int) bool {
	return (p.Bits[i/64] & (1 << uint(i) % 64)) != 0
}

func FD_ZERO(p *syscall.FdSet) {
	for i := range p.Bits {
		p.Bits[i] = 0
	}
}

func ParseTimestamp(timestamp string) (time.Time, error) {
	return time.Parse("02/Jan/2006:15:04:05 -0700", timestamp)
}

func ParseRequest(request string) (string, string, string, error) {
	components := strings.SplitN(request, " ", 3)

	if len(components) != 3 {
		return "", "", "", fmt.Errorf("Three tokens required (have %d): %s",
			len(components), components,
		)
	}

	if _, ok := HTTP_METHODS[components[0]]; !ok {
		return "", "", "", fmt.Errorf("Unsupported HTTP method %s",
			components[0],
		)
	}

	return components[0], components[1], components[2], nil
}

func ParseResource(resource string) (string, error) {
	trimmedResource := strings.Trim(resource, "/")
	components := strings.Split(trimmedResource, "/")

	if len(components) < 1 {
		return "", fmt.Errorf("No section found in resource %s\n",
			resource,
		)
	}

	// Prepend a "/" so that the root shows up, otherwise it would just be ""
	// "/" is requested
	return "/" + components[0], nil
}

func ParseHTTPStatus(status string) (int, bool, error) {
	isError := false
	code, err := strconv.Atoi(status)

	if err != nil {
		return 0, false, err
	}

	if code != http.StatusContinue &&
		code != http.StatusSwitchingProtocols &&
		code != http.StatusOK &&
		code != http.StatusCreated &&
		code != http.StatusAccepted &&
		code != http.StatusNonAuthoritativeInfo &&
		code != http.StatusNoContent &&
		code != http.StatusResetContent &&
		code != http.StatusPartialContent &&
		code != http.StatusMultipleChoices &&
		code != http.StatusMovedPermanently &&
		code != http.StatusFound &&
		code != http.StatusSeeOther &&
		code != http.StatusNotModified &&
		code != http.StatusUseProxy &&
		code != http.StatusTemporaryRedirect &&
		code != http.StatusBadRequest &&
		code != http.StatusUnauthorized &&
		code != http.StatusPaymentRequired &&
		code != http.StatusForbidden &&
		code != http.StatusNotFound &&
		code != http.StatusMethodNotAllowed &&
		code != http.StatusNotAcceptable &&
		code != http.StatusProxyAuthRequired &&
		code != http.StatusRequestTimeout &&
		code != http.StatusConflict &&
		code != http.StatusGone &&
		code != http.StatusLengthRequired &&
		code != http.StatusPreconditionFailed &&
		code != http.StatusRequestEntityTooLarge &&
		code != http.StatusRequestURITooLong &&
		code != http.StatusUnsupportedMediaType &&
		code != http.StatusRequestedRangeNotSatisfiable &&
		code != http.StatusExpectationFailed &&
		code != http.StatusTeapot &&
		code != http.StatusInternalServerError &&
		code != http.StatusNotImplemented &&
		code != http.StatusBadGateway &&
		code != http.StatusServiceUnavailable &&
		code != http.StatusGatewayTimeout &&
		code != http.StatusHTTPVersionNotSupported {
		return 0, false, errors.New("Unknown HTTP status code")
	}

	if code == http.StatusBadRequest ||
		code == http.StatusUnauthorized ||
		code == http.StatusPaymentRequired ||
		code == http.StatusForbidden ||
		code == http.StatusNotFound ||
		code == http.StatusMethodNotAllowed ||
		code == http.StatusNotAcceptable ||
		code == http.StatusProxyAuthRequired ||
		code == http.StatusRequestTimeout ||
		code == http.StatusConflict ||
		code == http.StatusGone ||
		code == http.StatusLengthRequired ||
		code == http.StatusPreconditionFailed ||
		code == http.StatusRequestEntityTooLarge ||
		code == http.StatusRequestURITooLong ||
		code == http.StatusUnsupportedMediaType ||
		code == http.StatusRequestedRangeNotSatisfiable ||
		code == http.StatusExpectationFailed ||
		code == http.StatusTeapot ||
		code == http.StatusInternalServerError ||
		code == http.StatusNotImplemented ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout ||
		code == http.StatusHTTPVersionNotSupported {
		isError = true
	}

	return code, isError, nil
}

func WatchFile(lines chan string, watching chan bool) {
	// You could use fsnotify here, but I'm sticking to the stdlib

	var buf bytes.Buffer
	inputBuffer := make([]byte, 1024)

	_, err := syscall.Seek(WEB_SERVICE.LogFileFD, 0, os.SEEK_END)

	if err != nil {
		log.Fatalf("Error seeking to the end of file %s: %s\n",
			WEB_SERVICE.LogFilePath, err,
		)
	}

	rfds := &syscall.FdSet{}

	watching <- true

	for {

		FD_ZERO(rfds)
		FD_SET(rfds, WEB_SERVICE.LogFileFD)

		_, err := syscall.Select(WEB_SERVICE.LogFileFD+1, rfds, nil, nil, nil)
		time.Sleep(time.Duration(SELECT_SLEEP_MS) * time.Millisecond)

		if err != nil {
			log.Fatalln(err)
		}

		if FD_ISSET(rfds, WEB_SERVICE.LogFileFD) {
			n, err := syscall.Read(WEB_SERVICE.LogFileFD, inputBuffer)

			if err != nil {
				log.Printf("Error reading from file %s: %s\n",
					WEB_SERVICE.LogFilePath, err,
				)
				continue
			}

			if n <= 0 {
				continue
			}

			buf.Write(inputBuffer[:n])

			for {
				line, err := buf.ReadString('\n')

				if err != nil {
					if err != io.EOF {
						log.Fatalf("Error reading from buffer (file %s): %s\n",
							WEB_SERVICE.LogFilePath, err,
						)
					}

					buf.Reset()
					if len(line) > 0 {
						buf.WriteString(line)
					}
					break
				} else {
					lines <- line
				}
			}
		}
	}
}

func ParseLine(lines chan string, events chan Event) {
	for {
		var components []string
		var isError bool
		referer := ""
		userAgent := ""
		isExtended := false
		byteCount := 0

		line := strings.TrimSpace(<-lines)

		if len(line) == 0 {
			continue
		}

		components = ECLF_REGEXP.FindStringSubmatch(line)
		if components != nil {
			isExtended = true
		} else {
			components = CLF_REGEXP.FindStringSubmatch(line)
		}
		if components == nil {
			log.Printf("Log line malformed [%s]\n", line)
			continue
		}

		host := components[1]

		timestamp, err := ParseTimestamp(components[4])

		if err != nil {
			log.Printf("Invalid timestamp [%s] (%s)\n",
				components[4], err,
			)
			continue
		}

		method, resource, version, err := ParseRequest(components[5])

		if err != nil {
			log.Printf("Invalid request {%s} [%s] (%s)\n",
				line, components[5], err,
			)
			continue
		}

		sectionName, err := ParseResource(resource)

		if err != nil {
			log.Printf("Invalid resource [%s] (%s)\n",
				resource, err,
			)
			continue
		}

		status, isError, err := ParseHTTPStatus(components[6])

		if err != nil {
			log.Printf("Invalid status [%s] (%s)\n",
				components[6], err,
			)
			continue
		}

		if components[7] != "-" {
			byteCount, err = strconv.Atoi(components[7])

			if err != nil {
				log.Printf("Invalid byte count [%s] (%s)\n",
					components[7], err,
				)
				continue
			}
		}

		if isExtended {
			referer = components[8]
			userAgent = components[9]
		}

		events <- Event{
			sectionName,
			host,
			timestamp,
			method,
			resource,
			version,
			byteCount,
			status,
			referer,
			userAgent,
			isError,
		}

	}
}

func HandleEvents(events chan Event) error {
	for {
		event := <-events

		section, ok := WEB_SERVICE.Sections[event.SectionName]

		if ok {
			section.HitCount++
			section.BytesTransferred += event.Bytes
			if event.IsError {
				section.ErrorCount++
			}
		} else {
			errorCount := 0
			if event.IsError {
				errorCount++
			}
			section = &Section{event.SectionName, 1, event.Bytes, errorCount}
			WEB_SERVICE.Sections[event.SectionName] = section
		}

		if WEB_SERVICE.BusiestSection == nil {
			WEB_SERVICE.BusiestSection = section
		} else if section != WEB_SERVICE.BusiestSection &&
			section.HitCount > WEB_SERVICE.BusiestSection.HitCount {
			WEB_SERVICE.BusiestSection = section
		}

		WEB_SERVICE.HitCount++
		WEB_SERVICE.BytesTransferred += event.Bytes
		if event.IsError {
			WEB_SERVICE.ErrorCount++
		}
	}
}

func PrintPeriodicStats(ticker <-chan time.Time) {
	for {
		currentTime := <-ticker

		if WEB_SERVICE.BusiestSection == nil {
			fmt.Printf("[%s] No activity\n", currentTime.Format(time.RFC1123))
		} else {
			fmt.Printf("[%s]: Transferred: %d KB | Errors: %d | Top Section: %s (%d)\n",
				currentTime.Format(time.RFC1123),
				WEB_SERVICE.BytesTransferred/1024,
				WEB_SERVICE.ErrorCount,
				WEB_SERVICE.BusiestSection.Name,
				WEB_SERVICE.BusiestSection.HitCount,
			)
		}

		WEB_SERVICE.BusiestSection = nil
		WEB_SERVICE.BytesTransferred = 0
		WEB_SERVICE.ErrorCount = 0

		for _, section := range WEB_SERVICE.Sections {
			section.HitCount = 0
			section.BytesTransferred = 0
			section.ErrorCount = 0
		}
	}
}

func WatchTraffic(ticker <-chan time.Time) {
	for {
		currentTime := <-ticker

		if WEB_SERVICE.HitCount > WEB_SERVICE.HitLimit {
			WEB_SERVICE.HighTraffic = true
			fmt.Printf(
				"High traffic generated an alert - hits = %d, "+
					"triggered at %s\n",
				WEB_SERVICE.HitCount,
				currentTime.Format(time.RFC1123),
			)
			WEB_SERVICE.HitCount = 0

		} else {
			if WEB_SERVICE.HighTraffic {
				fmt.Printf("High traffic subsided at %s\n",
					currentTime.Format(time.RFC1123),
				)
			}
			WEB_SERVICE.HighTraffic = false
			WEB_SERVICE.HitCount = 0
		}
	}
}

func SetWebService(logFilePath, errorLogFilePath string, hitLimit int) error {
	var errorLogFile *os.File = nil

	if WEB_SERVICE != nil {
		return errors.New("Can only create a web service once")
	}

	logFileFD, err := syscall.Open(logFilePath, os.O_RDONLY, 0666)

	if err != nil {
		log.Fatalf("Error opening file %s: %s\n", logFilePath, err)
	}

	logFile := os.NewFile(uintptr(logFileFD), logFilePath)

	if len(errorLogFilePath) > 0 {
		errorLogFile, err = os.OpenFile(
			errorLogFilePath,
			os.O_WRONLY|os.O_APPEND|os.O_CREATE,
			0660,
		)

		if err != nil {
			return fmt.Errorf("Error opening error log file %s: %s\n",
				errorLogFilePath, err,
			)
		}

		log.SetOutput(errorLogFile)
	}

	WEB_SERVICE = &WebService{
		logFilePath,
		logFile,
		logFileFD,
		errorLogFilePath,
		errorLogFile,
		nil,
		0,
		0,
		0,
		hitLimit,
		false,
		time.Time{},
		map[string]*Section{},
	}

	return nil
}

func printUsage() {
	fmt.Println("httptop\n")
	fmt.Println("Usage:\n")
	flag.PrintDefaults()
	fmt.Println("")
}

func main() {
	var logFilePath = flag.String(
		"file", "access.log", "Name of the log file to watch",
	)
	var errorLogFilePath = flag.String(
		"log", "", "File to log errors to",
	)
	var hitLimit = flag.Int(
		"trigger", 20, "Number of hits that constitutes high traffic",
	)
	var displayRate = flag.Duration(
		"rate",
		time.Duration(10)*time.Second,
		"Number of seconds between information summaries",
	)
	var highTrafficWindowSize = flag.Duration(
		"window",
		time.Duration(120)*time.Second,
		"Size of high traffic window in seconds",
	)
	var help = flag.Bool(
		"help",
		false,
		"Print usage information",
	)
	var cpuProfileFile = flag.String(
		"cpuprofile",
		"",
		"Profile CPU usage, save to this file",
	)

	flag.Parse()

	if *help {
		printUsage()
		os.Exit(0)
	}

	if *cpuProfileFile != "" {
		f, err := os.Create(*cpuProfileFile)

		if err != nil {
			log.Fatalf("Error opening CPU profiling file: %s\n", err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	done := make(chan bool)
	lines := make(chan string, 1024)
	events := make(chan Event, 1024)
	watching := make(chan bool)
	infoTicker := time.NewTicker(*displayRate)
	trafficTicker := time.NewTicker(*highTrafficWindowSize)

	err := SetWebService(*logFilePath, *errorLogFilePath, *hitLimit)

	if err != nil {
		printUsage()
		log.Fatalf("Error creating web service listener: %s\n", err)
	}

	fmt.Println("Welcome to HTTP Top!\n")

	go WatchFile(lines, watching)
	go ParseLine(lines, events)
	go HandleEvents(events)
	go PrintPeriodicStats(infoTicker.C)
	go WatchTraffic(trafficTicker.C)

	<-watching

	if *cpuProfileFile != "" {
		var input string
		fmt.Scanln(&input)
	} else {
		<-done
	}
}

/* vi: set noet ts=4 sw=4: */

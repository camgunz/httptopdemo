package main

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"os"
	"testing"
	"time"
)

// Go's testing framework changes the working directory, so relative paths
// don't work.  Therefore, these should be set in order to test properly
const DUMMY_ACCESS_LOG_PATH = "/home/charlie/code/dd/access.log"
const DUMMY_ERROR_LOG_PATH = "/home/charlie/code/dd/httplog.log"
const DUMMY_EVENT_LOG_PATH = "/home/charlie/code/dd/sample_logs/cg_access.log"

func getFakeDate() string {
	months := []string{
		"Jan", "Feb", "Mar", "Apr",
		"May", "Jun", "Jul", "Aug",
		"Sep", "Aug", "Nov", "Dec",
	}

	day := fmt.Sprintf("%02d", rand.Intn(11)+1)
	month := months[rand.Intn(len(months))]
	// Generate years inside the epoch
	year := fmt.Sprintf("%04d", rand.Intn(44)+1970)
	hour := fmt.Sprintf("%02d", rand.Intn(23))
	minute := fmt.Sprintf("%02d", rand.Intn(59))
	second := fmt.Sprintf("%02d", rand.Intn(59))
	timezone := fmt.Sprintf("%+05d", (rand.Intn(24)-12)*100)

	return fmt.Sprintf("[%s/%s/%s:%s:%s:%s %s]",
		day,
		month,
		year,
		hour,
		minute,
		second,
		timezone,
	)
}

func generateFakeLogLines(fakeFilePath string, limit int, randomizeEvents bool) {
	count := 0

	fakeFile, err := os.Open(fakeFilePath)
	if err != nil {
		log.Fatalf("Unable to open dummy log event file %s (%s)\n",
			fakeFilePath, err,
		)
	}

	eventFile, err := os.OpenFile(
		WEB_SERVICE.LogFilePath, os.O_WRONLY|os.O_APPEND, 0666,
	)
	if err != nil {
		log.Fatalf("Unable to open output log file %s: %s\n",
			WEB_SERVICE.LogFilePath, err,
		)
	}

	_, err = eventFile.Seek(0, os.SEEK_END)

	if err != nil {
		log.Fatalf("Unable to seek to the end of output log file %s: %s\n",
			WEB_SERVICE.LogFilePath, err,
		)
	}

	scanner := bufio.NewScanner(fakeFile)
	writer := bufio.NewWriter(eventFile)

	for scanner.Scan() {
		if randomizeEvents {
			// Add a little randomness to simulate a real webserver
			milliseconds := rand.Intn(5)

			time.Sleep(time.Duration(milliseconds) * time.Millisecond)
		}

		_, err := writer.WriteString(scanner.Text())
		if err != nil {
			log.Fatalf("Error writing to output log file: %s\n", err)
		}

		writer.WriteString("\n")

		err = writer.Flush()

		if err != nil {
			log.Fatalf("Error flushing output log file: %s\n", err)
		}

		count++

		if limit > 0 && count == limit {
			return
		}
	}

	if err = scanner.Err(); err != nil {
		log.Fatalf("Error generating fake events: %s\n", err)
	}
}

func TestParseTimestamp(t *testing.T) {
	for i := 0; i < 10000; i++ {
		ParseTimestamp(getFakeDate())
	}
}

func TestWatchTraffic(t *testing.T) {
	lines := make(chan string, 1024)
	events := make(chan Event, 1024)
	watching := make(chan bool)
	infoTicker := time.NewTicker(time.Duration(2) * time.Second)
	trafficTicker := time.NewTicker(time.Duration(5) * time.Second)

	err := SetWebService(DUMMY_ACCESS_LOG_PATH, "httptop.log", 24)

	if err != nil {
		log.Fatalf("Error creating web service listener: %s\n", err)
	}

	go WatchFile(lines, watching)
	go ParseLine(lines, events)
	go HandleEvents(events)
	go PrintPeriodicStats(infoTicker.C)
	go WatchTraffic(trafficTicker.C)

	<-watching

	generateFakeLogLines(DUMMY_EVENT_LOG_PATH, 0, true)
}

/* vi: set noet ts=4 sw=4: */

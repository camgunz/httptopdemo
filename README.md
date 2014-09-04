# HTTP Top (Demo)

This is a small demonstration app that analyzes an active log file (in Common
Log Format) and periodically prints statistics.

## Usage

  - `-cpuprofile=""`: Profile CPU usage, save to this file
  - `-file="access.log"`: Name of the log file to watch
  - `-help=false`: Print usage information
  - `-log=""`: File to log errors to
  - `-rate=10s`: Number of seconds between information summaries
  - `-trigger=20`: Number of hits that constitutes high traffic
  - `-window=2m0s`: Size of high traffic window in seconds

If `-log` is not used, logging is dumped to `stderr`, which can be messy.

## Testing

Testing is a little tricky because it relies on log files and Go's testing
framework changes the working directory of the app.  Inside `httptop_test.go`,
you'll find the following lines:

    const DUMMY_ACCESS_LOG_PATH = "/home/charlie/code/dd/access.log"
    const DUMMY_ERROR_LOG_PATH = "/home/charlie/code/dd/httplog.log"
    const DUMMY_EVENT_LOG_PATH = "/home/charlie/code/dd/sample_logs/cg_access.log"

Change them appropriately.  I know, suboptimal :(

## Implementation Improvements

  - Design an interface and avoid globals.
  - Don't presume a single log file; Go would make it easy to multiplex
    multiple logs
  - Use fsnotify and avoid the syscall package
  - Use a bona-fide console library instead of just dumping to console
  - Add the info ticker's duration to the web service listener so it could do
    per-second stats

## Design Improvements

  - I know httptop is a little contrived because it's an assignment, but I
    tried to implement the spec so that it could be extended beyond just
    printing out events to the console: sending SMS alerts, pushing events to a
    database, updating graph, etc.
  - I added a couple little improvements, like some command-line switches.
  - Add more in-depth stats during high-traffic times
    - Top 5 hosts/user-agents
  - Add bandwidth stats


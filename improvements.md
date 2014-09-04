## Implementation Improvements

  - Design an interface and avoid globals.
  - Don't presume a single log file; Go would make it easy to multiplex
    multiple logs
  - Use fsnotify and avoid the syscall package
  - Use a bona-fide console library instead of just dumping to console

## Design Improvements

  - I know httptop is a little contrived because it's an assignment, but I
    tried to implement the spec so that it could be extended beyond just
    printing out events to the console: sending SMS alerts, pushing events to a
    database, updating graph, etc.
  - I added a couple little improvements, like some command-line switches.
  - Add more in-depth stats during high-traffic times
    - Top 5 hosts/user-agents
  - Add bandwidth stats

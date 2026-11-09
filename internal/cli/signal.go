package cli

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyInterrupts relays Ctrl+C and termination requests to c.
func notifyInterrupts(c chan<- os.Signal) func() {
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	return func() { signal.Stop(c) }
}

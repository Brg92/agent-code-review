// Command agent-code-review reviews change requests and their related changes.
package main

import (
	"fmt"

	"github.com/Brg92/agent-code-review/internal/version"
)

func main() {
	fmt.Println(version.Version)
}

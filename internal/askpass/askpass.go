package askpass

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const modeEnvironment = "GIT_EMAIL_ASKPASS_MODE"

func Active() bool {
	return os.Getenv(modeEnvironment) == "1"
}

func Run(arguments []string, output io.Writer) error {
	prompt := strings.ToLower(strings.Join(arguments, " "))
	if strings.Contains(prompt, "username") {
		_, err := fmt.Fprintln(output, "x-access-token")
		return err
	}
	_, err := fmt.Fprintln(output, os.Getenv("GIT_EMAIL_ASKPASS_TOKEN"))
	return err
}

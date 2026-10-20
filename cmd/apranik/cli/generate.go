package cli

import (
	"crypto/rand"
	"flag"
	"fmt"
)

var cmdGenerate = &Command{
	Name:  "generate",
	Short: "generate a value a config file needs",
	Usage: `Usage: apranik generate <what>

Generate a value that belongs in a config file.

  uuid   a random RFC 4122 version 4 UUID

More kinds arrive with the modules that need them: a key pair is only worth
generating once a module reads one.`,
	Run: runGenerate,
}

func runGenerate(args []string) error {
	flags := flag.NewFlagSet("apranik generate", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		return fmt.Errorf("generate what? the kinds are: uuid")
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(1))
	}

	switch kind := flags.Arg(0); kind {
	case "uuid":
		value, err := newUUID()
		if err != nil {
			return err
		}
		fmt.Println(value)
		return nil
	default:
		return fmt.Errorf("cannot generate %q; the kinds are: uuid", kind)
	}
}

// RFC 4122 version 4
func newUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}

	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]), nil
}

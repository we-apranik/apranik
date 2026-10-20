package cli

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
)

// A function, not a var
func helpCommand() *Command {
	return &Command{
		Name:  "help",
		Short: "print this message, or detail on one command",
		Usage: `Usage: apranik help [command]

With no argument, list every command. With one, print that command's detail.`,
		Run: runHelp,
	}
}

func runHelp(args []string) error {
	flags := flag.NewFlagSet("apranik help", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}

	if flags.NArg() == 0 {
		overview()
		return nil
	}

	command := Lookup(flags.Arg(0))
	if command == nil {
		return fmt.Errorf("unknown command %q", flags.Arg(0))
	}

	fmt.Printf("%s - %s\n\n%s\n", command.Name, command.Short, command.Usage)
	return nil
}

func overview() {
	fmt.Print("apranik - an embeddable proxy core for censorship circumvention\n\nUsage: apranik <command> [flags]\n\nCommands:\n")

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	for _, command := range All() {
		fmt.Fprintf(writer, "  %s\t%s\n", command.Name, command.Short)
	}
	writer.Flush()

	fmt.Print("\nRun \"apranik help <command>\" for detail on one command.\n")
}

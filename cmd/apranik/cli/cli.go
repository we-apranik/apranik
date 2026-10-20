package cli

type Command struct {
	Name  string
	Short string // one line, for the command list
	Usage string // multi-line detail, printed by `apranik help <cmd>`
	Run   func(args []string) error
}

func All() []*Command {
	return []*Command{
		cmdRun,
		cmdCheck,
		cmdModules,
		cmdGenerate,
		cmdVersion,
		helpCommand(),
	}
}

func Lookup(name string) *Command {
	for _, command := range All() {
		if command.Name == name {
			return command
		}
	}
	return nil
}

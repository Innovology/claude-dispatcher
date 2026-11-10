package steward

import (
	"errors"
	"fmt"
	"io"
)

// Run is `claude-dispatcher steward …`; it returns the exit code.
//
//	steward                   every steward, newest first, with its state
//	steward new <product>     start a new steward conversation for a product
//	steward open <id>         reopen an older one where it left off
//	steward end <product|id>  end a product's steward (it stays reopenable)
func Run(args []string, out, errOut io.Writer) int {
	if err := run(args, out); err != nil {
		_, _ = fmt.Fprintln(errOut, "steward:", err)
		return 1
	}
	return 0
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "list" {
		return list(out)
	}
	if len(args) == 1 && (args[0] == "stop" || args[0] == "end") {
		// What `steward stop` meant before stewards were per product: the one
		// machine-wide steward, which this retires if it is still running.
		retired, err := RetireLegacy()
		if err != nil {
			return err
		}
		if retired {
			_, _ = fmt.Fprintln(out, "the machine-wide steward is retired · stewards are per product now: steward end <product>")
			return nil
		}
		return errors.New("name the product: steward end <product|id> (steward lists them)")
	}
	if len(args) != 2 {
		return errors.New("usage: steward [list] | new <product> | open <id> | end <product|id>")
	}
	switch args[0] {
	case "new", "start":
		s, err := Start(args[1])
		return report(out, s, err, "started")
	case "open", "reopen", "resume":
		s, err := Reopen(args[1])
		return report(out, s, err, "reopened")
	case "end", "stop":
		s, err := endByName(args[1])
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "%s's steward %s ended · steward open %s takes it back up\n", Key(s.Product), s.ID, s.ID)
		return nil
	}
	return fmt.Errorf("unknown argument %q (list | new | open | end)", args[0])
}

// endByName ends a steward named by id, else the current steward of a product
// with that name.
func endByName(name string) (*Steward, error) {
	if s, err := Find(name); err == nil {
		return End(s.ID)
	}
	return EndProduct(name)
}

func report(out io.Writer, s *Steward, err error, verb string) error {
	attach := "jump in from the cockpit (t) or tmux attach -t ="
	switch {
	case errors.Is(err, ErrRunning):
		_, _ = fmt.Fprintf(out, "%s's steward %s is already live · %s%s\n", Key(s.Product), s.ID, attach, SessionName(s.Product))
	case errors.Is(err, ErrUntrusted):
		_, _ = fmt.Fprintf(out, "%s's steward %s %s · tmux attach -t =%s\n", Key(s.Product), s.ID, err.Error(), SessionName(s.Product))
	case err != nil:
		return err
	default:
		_, _ = fmt.Fprintf(out, "%s's steward %s %s · %s%s\n", Key(s.Product), s.ID, verb, attach, SessionName(s.Product))
	}
	return nil
}

func list(out io.Writer) error {
	all := All()
	if len(all) == 0 {
		_, _ = fmt.Fprintln(out, "no stewards yet · steward new <product> starts one")
		return nil
	}
	for _, s := range all {
		_, _ = fmt.Fprintf(out, "%-12s %-12s %-24s opened %s\n", s.ID, StateOf(s), Key(s.Product), s.OpenedAt.Local().Format("2006-01-02 15:04"))
	}
	return nil
}

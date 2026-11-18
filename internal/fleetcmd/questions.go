package fleetcmd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/steward"
)

// options collects repeated --option flags.
type options []steward.Option

func (o *options) String() string { return fmt.Sprint(len(*o)) }

// Set reads "Label | what choosing it does"; the description is optional.
func (o *options) Set(v string) error {
	label, desc, _ := strings.Cut(v, "|")
	*o = append(*o, steward.Option{Label: strings.TrimSpace(label), Description: strings.TrimSpace(desc)})
	return nil
}

// awayAfter is the config's away window, read once per verb.
func awayAfter() time.Duration {
	cfg, err := loadConfig()
	if err != nil {
		cfg = nil
	}
	return cfg.AwayAfter()
}

// runAsk saves a question for the human (steward.Ask) and prints its id. A
// question asked about a dispatcher names it with --for, so the recap can say
// whose work waits on the answer.
func runAsk(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	product := fs.String("product", "", "the product it is about (required)")
	forKey := fs.String("for", "", "the dispatcher it holds up, by id or feature")
	context := fs.String("context", "", "what someone who has been away needs to answer it")
	var opts options
	fs.Var(&opts, "option", `an answer, "Label | what choosing it does" (2 to 4 of them for a choice)`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*product) == "" {
		return errors.New("name the product: --product <name>")
	}
	if cfg, err := loadConfig(); err == nil {
		if err := steward.CheckProduct(cfg, *product); err != nil {
			return err
		}
	}
	q := steward.Question{Text: strings.Join(fs.Args(), " "), Context: *context, Options: opts}
	if *forKey != "" {
		d, err := find(*forKey)
		if err != nil {
			return err
		}
		q.For, q.Feature = d.ID, d.Feature
	}
	q, added, err := steward.Ask(steward.Key(*product), q)
	if err != nil {
		return err
	}
	if !added {
		_, _ = fmt.Fprintf(out, "already saved as %s, asked %s · %s\n", q.ID, q.AskedAt.Local().Format("15:04"), q.Text)
		return nil
	}
	_, _ = fmt.Fprintf(out, "saved %s · %s\n", q.ID, q.Text)
	return nil
}

// runQuestions lists the questions saved for the human: the open ones, or all.
func runQuestions(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("questions", flag.ContinueOnError)
	product := fs.String("product", "", "the product (required)")
	asJSON := fs.Bool("json", false, "print them as JSON")
	all := fs.Bool("all", false, "include the answered ones")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*product) == "" {
		return errors.New("name the product: --product <name>")
	}
	t, away := time.Now(), awayAfter()
	qs := steward.OpenQuestions(steward.Key(*product), t, away)
	if *all {
		qs = steward.Questions(steward.Key(*product), t, away)
	}
	if *asJSON {
		if qs == nil {
			qs = []steward.Question{}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(qs)
	}
	if len(qs) == 0 {
		_, _ = fmt.Fprintln(out, "nothing saved for the human")
		return nil
	}
	for _, q := range qs {
		mark := "open"
		switch {
		case !q.Open():
			mark = "answered: " + q.Answer
		case q.Postponed:
			mark = "postponed"
		}
		who := ""
		if q.Feature != "" {
			who = " [" + q.Feature + "]"
		}
		_, _ = fmt.Fprintf(out, "%s  %s  asked %s%s · %s\n", q.ID, mark, q.AskedAt.Local().Format("Mon 15:04"), who, q.Text)
	}
	return nil
}

// runAnswer closes a saved question with the human's answer.
func runAnswer(args []string, out io.Writer) error {
	if len(args) < 2 {
		return errors.New("usage: answer <question id> <the human's answer…>")
	}
	_, q, err := steward.Answer(args[0], strings.Join(args[1:], " "))
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "answered %s · %s → %s\n", q.ID, q.Text, q.Answer)
	return nil
}

// questionsNow is the product's open questions for `status --next`, with the
// away window they are measured against.
func questionsNow(product string, t time.Time, cfg *config.Config) []steward.Question {
	if product == "" {
		return nil
	}
	return steward.OpenQuestions(steward.Key(product), t, cfg.AwayAfter())
}

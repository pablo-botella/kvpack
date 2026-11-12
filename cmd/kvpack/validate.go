package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pablo-botella/kvpack"
	"github.com/pablo-botella/kvpack/internal/schema"
)

// cmdValidate checks a message file against a declaration with the rules of
// the generated code and prints it with the declared names.
func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	reply := fs.Bool("reply", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("validate takes a .kvp file and a message file (flags go before them)")
	}
	f, err := load(fs.Arg(0))
	if err != nil {
		return err
	}
	b, err := os.ReadFile(fs.Arg(1))
	if err != nil {
		return err
	}
	n, err := validate(os.Stdout, f, b, *reply)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%s: %d problem(s)", fs.Arg(1), n)
	}
	return nil
}

// validate decodes b, picks the block its command number names (the reply
// of that command when reply is set) and prints it, returning how many
// problems a generated decoder would refuse it for.
func validate(w io.Writer, f *schema.File, b []byte, reply bool) (int, error) {
	m, err := kvpack.Decode(b)
	if err != nil {
		return 0, fmt.Errorf("not a valid message: %w", err)
	}
	var bl *schema.Block
	for _, c := range f.Blocks {
		if c.Number != m.Command {
			continue
		}
		if (reply && c.Kind == schema.Reply) || (!reply && c.Kind != schema.Reply) {
			bl = c
		}
	}
	if bl == nil {
		what := "command or record"
		if reply {
			what = "reply"
		}
		return 0, fmt.Errorf("no %s with number %d in %s", what, m.Command, f.Name)
	}
	if reply {
		if text, bad := m.ErrorText(); bad {
			fmt.Fprintf(w, "reply %s (%d): error reply: %s\n", bl.Name, bl.Number, strconv.Quote(text))
			return 0, nil
		}
	}
	fmt.Fprintf(w, "%s %s (%d), %d items\n", bl.Kind, bl.Name, bl.Number, len(m.Items))
	return dump(w, bl, m, "  "), nil
}

// dump prints the fields of bl as found in m and returns the problems.
func dump(w io.Writer, bl *schema.Block, m *kvpack.Message, ind string) int {
	problems := 0
	width := 0
	declared := map[uint16]bool{}
	for _, fl := range bl.Fields {
		width = max(width, len(fl.Name))
		declared[fl.Key] = true
	}
	for _, fl := range bl.Fields {
		label := fmt.Sprintf("%s%-*s %-6s =%3d  ", ind, width, fl.Name, typeName(fl), fl.Key)
		items := m.FindAll(fl.Key)
		want := wantType(fl)
		if fl.Type == schema.TupleSet {
			ts, ok := m.TupleSet(fl.Key)
			switch {
			case ok:
				fmt.Fprintf(w, "%s%d fields, %d tuples\n", label, len(ts.Def.Fields), len(ts.Rows))
				for _, f := range ts.Def.Fields {
					fmt.Fprintf(w, "%s    %s %s\n", ind, f.Name, f.Type)
				}
			case fl.Required():
				fmt.Fprintf(w, "%sMISSING\n", label)
				problems++
			default:
				fmt.Fprintf(w, "%sabsent (optional)\n", label)
			}
			continue
		}
		if fl.List {
			fmt.Fprintf(w, "%s[%d]\n", label, len(items))
			for i, it := range items {
				if it.Type != want {
					fmt.Fprintf(w, "%s  [%d] skipped: %s, not %s\n", ind, i, it.Type, want)
					continue
				}
				fmt.Fprintf(w, "%s  [%d] ", ind, i)
				problems += value(w, fl, it, ind+"      ")
			}
			continue
		}
		switch {
		case len(items) == 0 && fl.Required():
			fmt.Fprintf(w, "%sMISSING\n", label)
			problems++
		case len(items) == 0 && fl.Nullable:
			fmt.Fprintf(w, "%sabsent: null (optional nullable)\n", label)
		case len(items) == 0:
			fmt.Fprintf(w, "%sabsent (optional)\n", label)
		case fl.Nullable && items[0].Type == kvpack.Null:
			fmt.Fprintf(w, "%snull\n", label)
		case items[0].Type != want:
			if fl.Required() {
				fmt.Fprintf(w, "%sMISSING: found %s, not %s\n", label, items[0].Type, want)
				problems++
			} else {
				fmt.Fprintf(w, "%sabsent: found %s, not %s (optional)\n", label, items[0].Type, want)
			}
		default:
			fmt.Fprint(w, label)
			problems += value(w, fl, items[0], ind+"    ")
			if len(items) > 1 {
				fmt.Fprintf(w, "%s  (key repeated %d times: the first one is used)\n", ind, len(items))
			}
		}
	}
	for _, it := range m.Items {
		if !declared[it.Key] && it.Key != kvpack.KeyError {
			fmt.Fprintf(w, "%s? key %d %s: not declared, ignored\n", ind, it.Key, it.Type)
		}
	}
	return problems
}

// value prints one item of the declared type and, for a record, its fields.
func value(w io.Writer, fl *schema.Field, it *kvpack.Item, ind string) int {
	switch fl.Type {
	case schema.Int32:
		v, _ := it.Int32()
		fmt.Fprintln(w, v)
	case schema.Int64:
		v, _ := it.Int64()
		fmt.Fprintln(w, v)
	case schema.TS:
		v, _ := it.Time()
		fmt.Fprintf(w, "%s (%d)\n", v.Format(time.RFC3339), v.Unix())
	case schema.Text:
		v, _ := it.Text()
		if len(v) > 80 {
			v = v[:80] + "..."
		}
		fmt.Fprintln(w, strconv.Quote(v))
	case schema.Bin:
		v, _ := it.Bin()
		s := hex.EncodeToString(v[:min(len(v), 16)])
		if len(v) > 16 {
			s += "..."
		}
		fmt.Fprintf(w, "%d bytes %s\n", len(v), s)
	case schema.Bool:
		v, _ := it.Bool()
		fmt.Fprintln(w, v)
	case schema.Float64:
		v, _ := it.Float64()
		fmt.Fprintln(w, v)
	case schema.DS:
		v, _ := it.Date()
		if v.IsZero() {
			fmt.Fprintln(w, "null date")
		} else {
			fmt.Fprintln(w, v.Format("2006-01-02"))
		}
	case schema.Msg:
		sub, _ := it.Message()
		fmt.Fprintf(w, "message %d, %d items\n", sub.Command, len(sub.Items))
	case schema.Rec:
		sub, _ := it.Message()
		fmt.Fprintf(w, "record %s (%d)\n", fl.Record.Name, sub.Command)
		return dump(w, fl.Record, sub, ind)
	}
	return 0
}

func wantType(fl *schema.Field) kvpack.Type {
	switch fl.Type {
	case schema.Int32:
		return kvpack.Int32
	case schema.Int64:
		return kvpack.Int64
	case schema.TS:
		return kvpack.Time
	case schema.Text:
		return kvpack.Text
	case schema.Bin:
		return kvpack.Bin
	case schema.Bool:
		return kvpack.Bool
	case schema.Float64:
		return kvpack.Float64
	case schema.DS:
		return kvpack.Date
	}
	return kvpack.Msg
}

func typeName(fl *schema.Field) string {
	if fl.Type == schema.Rec {
		return fl.Record.Name
	}
	return strings.ToLower(fl.Type)
}

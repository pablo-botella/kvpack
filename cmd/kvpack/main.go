// Command kvpack checks a .kvp declaration, generates the code that packs
// and unpacks its commands, replies and records — Go, C (over capi/kvpack.h)
// and Harbour (over the hbkvpack wrapper) — and validates message files
// against it.
//
//	kvpack check api.kvp
//	kvpack gen [-go api_kvp.go] [-pkg api] [-c api_kvp.h] [-hb api_kvp.prg] [-prefix api] api.kvp
//	kvpack validate [-reply] api.kvp message.bin
//	kvpack version
//
// -c writes the header and, next to it, the .c of the same name; -hb writes
// the .prg and the .ch of the same name. -prefix starts the C and Harbour
// names and -pkg is the Go package; both default to the .kvp file name.
// validate applies the rules of the generated code to one message and
// prints it with the declared names.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/pablo-botella/kvpack/internal/gen"
	"github.com/pablo-botella/kvpack/internal/schema"
)

const usage = `kvpack checks a .kvp declaration, generates code from it and validates
messages against it.

usage:
  kvpack check file.kvp
  kvpack gen [flags] file.kvp
  kvpack validate [-reply] file.kvp message.bin
  kvpack version

gen flags:
  -go file      write Go code (package -pkg)
  -pkg name     Go package name (default: the .kvp base name)
  -c file.h     write a C header and, next to it, the .c of the same name
  -hb file.prg  write Harbour functions and, next to them, the .ch of the same name
  -prefix p     prefix of C and Harbour names (default: the .kvp base name)

validate reads the message as the command (or record) its number names, or
as that command's reply with -reply, and fails when a generated decoder
would refuse it.
`

func main() {
	// mkskill owns the generate-* documentation commands: CheckParams runs
	// one if kvpack was invoked as such, else falls through.
	if err, done := MkskillSpec.CheckParams(); done {
		return
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "kvpack:", err)
		os.Exit(1)
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kvpack:", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, usage)
	fmt.Fprintf(os.Stderr, "  %s\n", strings.ReplaceAll(MkskillSpec.Usage(false), "\n", "\n  "))
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		os.Exit(2)
	}
	switch args[0] {
	case "check":
		if len(args) != 2 {
			return fmt.Errorf("check takes one .kvp file")
		}
		f, err := load(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("%s: %d commands, %d replies, %d records\n", args[1], len(f.Commands()), len(f.Replies()), len(f.Records()))
		return nil
	case "gen":
		return generate(args[1:])
	case "validate":
		return cmdValidate(args[1:])
	case "version":
		// the embed says which release this claims to be; the build info
		// says what was actually compiled
		if MkskillSpec.Version != "" {
			fmt.Println("kvpack " + MkskillSpec.Version + " (" + buildVersion() + ")")
		} else {
			fmt.Println("kvpack " + buildVersion())
		}
		return nil
	case "-h", "-help", "--help", "help":
		printUsage()
		return nil
	}
	return fmt.Errorf("unknown command %q (check, gen, validate, version)", args[0])
}

// buildVersion is the module version the binary was compiled at: a tagged
// build or `go install ...@vX.Y.Z` reports the tag, a local build "dev".
func buildVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi.Main.Version == "" || bi.Main.Version == "(devel)" {
		return "dev"
	}
	return bi.Main.Version
}

func generate(args []string) error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	goOut := fs.String("go", "", "")
	pkg := fs.String("pkg", "", "")
	cOut := fs.String("c", "", "")
	hbOut := fs.String("hb", "", "")
	prefix := fs.String("prefix", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("gen takes one .kvp file (flags go before it)")
	}
	if *goOut == "" && *cOut == "" && *hbOut == "" {
		return fmt.Errorf("gen: nothing to write (-go, -c or -hb)")
	}
	src := fs.Arg(0)
	f, err := load(src)
	if err != nil {
		return err
	}
	base := defaultName(src)
	if *pkg == "" {
		*pkg = base
	}
	if *prefix == "" {
		*prefix = base
	}
	if *goOut != "" {
		b, err := gen.Go(f, *pkg)
		if err != nil {
			return err
		}
		if err := write(*goOut, b); err != nil {
			return err
		}
	}
	if *cOut != "" {
		h, c, err := gen.C(f, *prefix, *cOut)
		if err != nil {
			return err
		}
		if err := write(*cOut, h); err != nil {
			return err
		}
		if err := write(swapExt(*cOut, ".c"), c); err != nil {
			return err
		}
	}
	if *hbOut != "" {
		ch := swapExt(*hbOut, ".ch")
		chb, prg, err := gen.Harbour(f, *prefix, ch)
		if err != nil {
			return err
		}
		if err := write(ch, chb); err != nil {
			return err
		}
		if err := write(*hbOut, prg); err != nil {
			return err
		}
	}
	return nil
}

func load(path string) (*schema.File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return schema.Parse(path, fh)
}

// defaultName is the .kvp base name as an identifier: "my-api.kvp" -> "my_api".
func defaultName(path string) string {
	n := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	n = regexp.MustCompile(`[^a-z0-9_]`).ReplaceAllString(strings.ToLower(n), "_")
	if n == "" || n[0] < 'a' || n[0] > 'z' {
		n = "kvp_" + n
	}
	return n
}

func swapExt(path, ext string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ext
}

// write leaves a file untouched when its content is already b, so a build
// that depends on timestamps does not rebuild for nothing.
func write(path string, b []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return nil
	}
	return os.WriteFile(path, b, 0o644)
}

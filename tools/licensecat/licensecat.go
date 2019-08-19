// Command licensecat finds all license files and other legal notices
// for all third-party Go packages in the vendor directory, and prints
// the contatenated result to stdout.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"regexp"
	"strings"
)

const (
	// moduleFile is the modules file written by the "go mod" tool.
	moduleFile = "modules.txt"

	// vendorDir is the directory where third-party Go packages live.
	vendorDir = "vendor"
)

var (
	// licNames are possible names for the license file. At least
	// one of them is required to exist.
	licNames = []string{"LICENSE", "LICENSE.txt", "LICENCE", "LICENSE.md", "COPYRIGHT"}

	// extraNames are possible names for additional legal files.
	extraNames = []string{"PATENTS", "NOTICE"}
)

// main is the entry point.
func main() {
	log.SetFlags(0)
	log.SetPrefix("licensecat: ")
	flag.Parse()

	if len(os.Args) > 2 {
		log.Fatal("error: must provide a single top-level directory")
	}

	dirname := "."
	if len(os.Args) == 2 {
		dirname = os.Args[1]
	}

	if err := run(dirname); err != nil {
		log.Fatal(fmt.Sprintf("error: %v", err))
	}
}

// run prints concatenated license text to stdout.
func run(dirname string) error {
	b, err := ioutil.ReadFile(path.Join(dirname, vendorDir, moduleFile))
	if err != nil {
		return err
	}

	names, err := getPackageNames(b)
	if err != nil {
		return err
	}

	out := []string{}
	for _, name := range names {
		fname, lic, err := getLicense(dirname, name)
		if err != nil {
			return err
		}
		out = append(out, fmtLicense(fname, lic))
		for _, f := range extraNames {
			fname := path.Join(dirname, vendorDir, name, f)
			b, err := ioutil.ReadFile(fname)
			if err != nil {
				continue
			}
			out = append(out, fmtLicense(fname, string(b)))
		}
	}
	fmt.Println(strings.Join(out, "\n"))
	return nil
}

// getPackageNames parses vendor/modules.txt and returns the Go package names.
func getPackageNames(data []byte) ([]string, error) {
	// Using regex to parse package names for now
	// until "cmd/go/internal/modfile" is exposed to external
	// https://github.com/golang/go/issues/31761
	names := []string{}
	re := regexp.MustCompile(`(?m:^# (?P<name>\S+) (?P<version>\S+).*$)`)

	matches := re.FindAllSubmatch(data, -1)
	for _, match := range matches {
		names = append(names, string(match[1]))
	}
	return names, nil
}

// getLicense finds and returns the license file for a given package.
func getLicense(dirname, name string) (string, string, error) {
	for _, f := range licNames {
		fname := path.Join(dirname, vendorDir, name, f)
		b, err := ioutil.ReadFile(fname)
		if err != nil {
			continue
		}
		return fname, string(b), nil
	}
	return "", "", fmt.Errorf("cannot find license for package: %v", name)
}

// fmtLicense returns a string containing a header and the license text.
func fmtLicense(fname, text string) string {
	return fmt.Sprintf("# %v\n\n%v", fname, text)
}

package goconf

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"
)

// ReadConfigFile reads a file and returns a new configuration representation.
// This representation can be queried with GetString, etc.
func ReadConfigFile(fname string) (c *ConfigFile, err error) {
	var file *os.File

	if file, err = os.Open(fname); err != nil {
		return nil, err
	}

	c = NewConfigFile()
	if err = c.Read(file); err != nil {
		return nil, err
	}

	if err = file.Close(); err != nil {
		return nil, err
	}

	return c, nil
}

func ReadConfigBytes(conf []byte) (c *ConfigFile, err error) {
	buf := bytes.NewBuffer(conf)

	c = NewConfigFile()
	if err = c.Read(buf); err != nil {
		return nil, err
	}

	return c, err
}

// Read reads an io.Reader and returns a configuration representation. This
// representation can be queried with GetString, etc.
func (c *ConfigFile) Read(reader io.Reader) (err error) {
	buf := bufio.NewReader(reader)

	var section, option string
	section = "default"
	for {
		l, buferr := buf.ReadString('\n') // parse line-by-line
		l = strings.TrimSpace(l)

		if buferr != nil {
			if buferr != io.EOF {
				return err
			}

			if len(l) == 0 {
				break
			}
		}

		// switch written for readability (not performance)
		switch {
		case len(l) == 0: // empty line
			continue

		case l[0] == '#': // comment
			continue

		case l[0] == ';': // comment
			continue

		case len(l) >= 3 && strings.ToLower(l[0:3]) == "rem": // comment (for windows users)
			continue

		case l[0] == '[' && l[len(l)-1] == ']': // new section
			option = "" // reset multi-line value
			section = strings.TrimSpace(l[1 : len(l)-1])
			c.AddSection(section)

		case section == "": // not new section and no section defined so far
			return ReadError{BlankSection, l}

		default: // other alternatives
			i := strings.IndexAny(l, "=:")
			switch {
			case i > 0: // option and value
				i := strings.IndexAny(l, "=:")
				option = strings.TrimSpace(l[0:i])
				value := strings.TrimSpace(stripComments(l[i+1:]))
				c.AddOption(section, option, value)

			case section != "" && option != "": // continuation of multi-line value
				prev, _ := c.GetRawString(section, option)
				value := strings.TrimSpace(stripComments(l))
				c.AddOption(section, option, prev+"\n"+value)

			default:
				return ReadError{CouldNotParse, l}
			}
		}

		// Reached end of file
		if buferr == io.EOF {
			break
		}
	}
	return nil
}

func stripComments(l string) string {
	// comments are preceded by space or TAB
	for _, c := range []string{" ;", "\t;", " #", "\t#"} {
		if i := strings.Index(l, c); i != -1 {
			l = l[0:i]
		}
	}
	return l
}

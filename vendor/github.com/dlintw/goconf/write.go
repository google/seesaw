package goconf

import (
	"bytes"
	"io"
	"os"
)

// WriteConfigFile saves the configuration representation to a file.
// The desired file permissions must be passed as in os.Open.
// The header is a string that is saved as a comment in the first line of the file.
func (c *ConfigFile) WriteConfigFile(fname string, perm uint32, header string) (err error) {
	var file *os.File

	if file, err = os.Create(fname); err != nil {
		return err
	}
	if err = c.Write(file, header); err != nil {
		return err
	}

	return file.Close()
}

// WriteConfigBytes returns the configuration file.
func (c *ConfigFile) WriteConfigBytes(header string) (config []byte) {
	buf := bytes.NewBuffer(nil)

	c.Write(buf, header)

	return buf.Bytes()
}

// Writes the configuration file to the io.Writer.
func (c *ConfigFile) Write(writer io.Writer, header string) (err error) {
	buf := bytes.NewBuffer(nil)

	if header != "" {
		if _, err = buf.WriteString("# " + header + "\n"); err != nil {
			return err
		}
	}

	for section, sectionmap := range c.data {
		if section == DefaultSection && len(sectionmap) == 0 {
			continue // skip default section if empty
		}
		if _, err = buf.WriteString("[" + section + "]\n"); err != nil {
			return err
		}
		for option, value := range sectionmap {
			if _, err = buf.WriteString(option + "=" + value + "\n"); err != nil {
				return err
			}
		}
		if _, err = buf.WriteString("\n"); err != nil {
			return err
		}
	}

	buf.WriteTo(writer)

	return nil
}

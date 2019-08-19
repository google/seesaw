package goconf

import (
	"strconv"
	"strings"
)

// GetSections returns the list of sections in the configuration.
// (The default section always exists.)
func (c *ConfigFile) GetSections() (sections []string) {
	sections = make([]string, len(c.data))

	i := 0
	for s := range c.data {
		sections[i] = s
		i++
	}

	return sections
}

// HasSection checks if the configuration has the given section.
// (The default section always exists.)
func (c *ConfigFile) HasSection(section string) bool {
	if section == "" {
		section = "default"
	}
	_, ok := c.data[strings.ToLower(section)]

	return ok
}

// GetOptions returns the list of options available in the given section.
// It returns an error if the section does not exist and an empty list if the section is empty.
// Options within the default section are also included.
func (c *ConfigFile) GetOptions(section string) (options []string, err error) {
	if section == "" {
		section = "default"
	}
	section = strings.ToLower(section)

	if _, ok := c.data[section]; !ok {
		return nil, GetError{SectionNotFound, "", "", section, ""}
	}

	options = make([]string, len(c.data[DefaultSection])+len(c.data[section]))
	i := 0
	for s := range c.data[DefaultSection] {
		options[i] = s
		i++
	}
	for s := range c.data[section] {
		options[i] = s
		i++
	}

	return options, nil
}

// HasOption checks if the configuration has the given option in the section.
// It returns false if either the option or section do not exist.
func (c *ConfigFile) HasOption(section string, option string) bool {
	if section == "" {
		section = "default"
	}
	section = strings.ToLower(section)
	option = strings.ToLower(option)

	if _, ok := c.data[section]; !ok {
		return false
	}

	_, okd := c.data[DefaultSection][option]
	_, oknd := c.data[section][option]

	return okd || oknd
}

// GetRawString gets the (raw) string value for the given option in the section.
// The raw string value is not subjected to unfolding, which was illustrated in the beginning of this documentation.
// It returns an error if either the section or the option do not exist.
func (c *ConfigFile) GetRawString(section string, option string) (value string, err error) {
	if section == "" {
		section = "default"
	}

	section = strings.ToLower(section)
	option = strings.ToLower(option)

	if _, ok := c.data[section]; ok {
		if value, ok = c.data[section][option]; ok {
			return value, nil
		}
		return "", GetError{OptionNotFound, "", "", section, option}
	}
	return "", GetError{SectionNotFound, "", "", section, option}
}

// GetString gets the string value for the given option in the section.
// If the value needs to be unfolded (see e.g. %(host)s example in the beginning of this documentation),
// then GetString does this unfolding automatically, up to DepthValues number of iterations.
// It returns an error if either the section or the option do not exist, or the unfolding cycled.
func (c *ConfigFile) GetString(section string, option string) (value string, err error) {
	value, err = c.GetRawString(section, option)
	if err != nil {
		return "", err
	}

	section = strings.ToLower(section)

	var i int

	for i = 0; i < DepthValues; i++ { // keep a sane depth
		loc := varRegExp.FindStringIndex(value)
		if loc == nil {
			break
		}
		noption := value[loc[0]+2 : loc[1]-2] // "%(host)s"->"host"
		noption = strings.ToLower(noption)

		nvalue, _ := c.data[DefaultSection][noption] // search variable in default section
		if _, ok := c.data[section][noption]; ok {
			nvalue = c.data[section][noption]
		}
		if nvalue == "" {
			return "", GetError{OptionNotFound, "", "", section, option}
		}

		// substitute by new value and take off leading '%(' and trailing ')s'
		value = value[0:loc[0]] + nvalue + value[loc[1]:]
	}

	if i == DepthValues {
		return "", GetError{MaxDepthReached, "", "", section, option}
	}

	return value, nil
}

// GetInt has the same behaviour as GetString but converts the response to int.
func (c *ConfigFile) GetInt(section string, option string) (value int, err error) {
	sv, err := c.GetString(section, option)
	if err == nil {
		value, err = strconv.Atoi(sv)
		if err != nil {
			err = GetError{CouldNotParse, "int", sv, section, option}
		}
	}

	return value, err
}

// GetFloat has the same behaviour as GetString but converts the response to float.
func (c *ConfigFile) GetFloat64(section string, option string) (value float64, err error) {
	sv, err := c.GetString(section, option)
	if err == nil {
		value, err = strconv.ParseFloat(sv, 64)
		if err != nil {
			err = GetError{CouldNotParse, "float64", sv, section, option}
		}
	}

	return value, err
}

// GetBool has the same behaviour as GetString but converts the response to bool.
// See constant BoolStrings for string values converted to bool.
func (c *ConfigFile) GetBool(section string, option string) (value bool, err error) {
	sv, err := c.GetString(section, option)
	if err != nil {
		return false, err
	}

	value, ok := BoolStrings[strings.ToLower(sv)]
	if !ok {
		return false, GetError{CouldNotParse, "bool", sv, section, option}
	}

	return value, nil
}

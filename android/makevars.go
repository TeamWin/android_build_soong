// Copyright 2016 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package android

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/pathtools"
	"github.com/google/blueprint/proptools"
)

func init() {
	RegisterMakeVarsProvider(pctx, androidMakeVarsProvider)
}

func androidMakeVarsProvider(ctx MakeVarsContext) {
	ctx.Strict("MIN_SUPPORTED_SDK_VERSION", strconv.Itoa(ctx.Config().MinSupportedSdkVersion()))
}

///////////////////////////////////////////////////////////////////////////////
// Interface for other packages to use to declare make variables
type MakeVarsContext interface {
	Config() Config
	DeviceConfig() DeviceConfig
	AddNinjaFileDeps(deps ...string)
	Fs() pathtools.FileSystem

	ModuleName(module blueprint.Module) string
	ModuleDir(module blueprint.Module) string
	ModuleSubDir(module blueprint.Module) string
	ModuleType(module blueprint.Module) string
	BlueprintFile(module blueprint.Module) string

	ModuleErrorf(module blueprint.Module, format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Failed() bool

	VisitAllModules(visit func(Module))
	VisitAllModulesIf(pred func(Module) bool, visit func(Module))

	// Verify the make variable matches the Soong version, fail the build
	// if it does not. If the make variable is empty, just set it.
	Strict(name, ninjaStr string)
	// Check to see if the make variable matches the Soong version, warn if
	// it does not. If the make variable is empty, just set it.
	Check(name, ninjaStr string)

	// These are equivalent to the above, but sort the make and soong
	// variables before comparing them. They also show the unique entries
	// in each list when displaying the difference, instead of the entire
	// string.
	StrictSorted(name, ninjaStr string)
	CheckSorted(name, ninjaStr string)

	// Evaluates a ninja string and returns the result. Used if more
	// complicated modification needs to happen before giving it to Make.
	Eval(ninjaStr string) (string, error)

	// These are equivalent to Strict and Check, but do not attempt to
	// evaluate the values before writing them to the Makefile. They can
	// be used when all ninja variables have already been evaluated through
	// Eval().
	StrictRaw(name, value string)
	CheckRaw(name, value string)

	// GlobWithDeps returns a list of files that match the specified pattern but do not match any
	// of the patterns in excludes.  It also adds efficient dependencies to rerun the primary
	// builder whenever a file matching the pattern as added or removed, without rerunning if a
	// file that does not match the pattern is added to a searched directory.
	GlobWithDeps(pattern string, excludes []string) ([]string, error)
}

var _ PathContext = MakeVarsContext(nil)

type MakeVarsProvider func(ctx MakeVarsContext)

func RegisterMakeVarsProvider(pctx PackageContext, provider MakeVarsProvider) {
	makeVarsProviders = append(makeVarsProviders, makeVarsProvider{pctx, provider})
}

// SingletonMakeVarsProvider is a Singleton with an extra method to provide extra values to be exported to Make.
type SingletonMakeVarsProvider interface {
	Singleton

	// MakeVars uses a MakeVarsContext to provide extra values to be exported to Make.
	MakeVars(ctx MakeVarsContext)
}

// registerSingletonMakeVarsProvider adds a singleton that implements SingletonMakeVarsProvider to the list of
// MakeVarsProviders to run.
func registerSingletonMakeVarsProvider(singleton SingletonMakeVarsProvider) {
	makeVarsProviders = append(makeVarsProviders, makeVarsProvider{pctx, SingletonmakeVarsProviderAdapter(singleton)})
}

// SingletonmakeVarsProviderAdapter converts a SingletonMakeVarsProvider to a MakeVarsProvider.
func SingletonmakeVarsProviderAdapter(singleton SingletonMakeVarsProvider) MakeVarsProvider {
	return func(ctx MakeVarsContext) { singleton.MakeVars(ctx) }
}

///////////////////////////////////////////////////////////////////////////////

func makeVarsSingletonFunc() Singleton {
	return &makeVarsSingleton{}
}

type makeVarsSingleton struct{}

type makeVarsProvider struct {
	pctx PackageContext
	call MakeVarsProvider
}

var makeVarsProviders []makeVarsProvider

type makeVarsContext struct {
	SingletonContext
	config Config
	pctx   PackageContext
	vars   []makeVarsVariable
}

var _ MakeVarsContext = &makeVarsContext{}

type makeVarsVariable struct {
	name   string
	value  string
	sort   bool
	strict bool
}

func (s *makeVarsSingleton) GenerateBuildActions(ctx SingletonContext) {
	if !ctx.Config().EmbeddedInMake() {
		return
	}

	outFile := PathForOutput(ctx, "make_vars"+proptools.String(ctx.Config().productVariables.Make_suffix)+".mk").String()

	if ctx.Failed() {
		return
	}

	vars := []makeVarsVariable{}
	for _, provider := range makeVarsProviders {
		mctx := &makeVarsContext{
			SingletonContext: ctx,
			pctx:             provider.pctx,
		}

		provider.call(mctx)

		vars = append(vars, mctx.vars...)
	}

	if ctx.Failed() {
		return
	}

	outBytes := s.writeVars(vars)

	if _, err := os.Stat(outFile); err == nil {
		if data, err := ioutil.ReadFile(outFile); err == nil {
			if bytes.Equal(data, outBytes) {
				return
			}
		}
	}

	if err := ioutil.WriteFile(outFile, outBytes, 0666); err != nil {
		ctx.Errorf(err.Error())
	}
}

func (s *makeVarsSingleton) writeVars(vars []makeVarsVariable) []byte {
	buf := &bytes.Buffer{}

	fmt.Fprint(buf, `# Autogenerated file

# Compares SOONG_$(1) against $(1), and warns if they are not equal.
#
# If the original variable is empty, then just set it to the SOONG_ version.
#
# $(1): Name of the variable to check
# $(2): If not-empty, sort the values before comparing
# $(3): Extra snippet to run if it does not match
define soong-compare-var
ifneq ($$($(1)),)
  my_val_make := $$(strip $(if $(2),$$(sort $$($(1))),$$($(1))))
  my_val_soong := $(if $(2),$$(sort $$(SOONG_$(1))),$$(SOONG_$(1)))
  ifneq ($$(my_val_make),$$(my_val_soong))
    $$(warning $(1) does not match between Make and Soong:)
    $(if $(2),$$(warning Make  adds: $$(filter-out $$(my_val_soong),$$(my_val_make))),$$(warning Make : $$(my_val_make)))
    $(if $(2),$$(warning Soong adds: $$(filter-out $$(my_val_make),$$(my_val_soong))),$$(warning Soong: $$(my_val_soong)))
    $(3)
  endif
  my_val_make :=
  my_val_soong :=
else
  $(1) := $$(SOONG_$(1))
endif
.KATI_READONLY := $(1) SOONG_$(1)
endef

my_check_failed := false

`)

	// Write all the strict checks out first so that if one of them errors,
	// we get all of the strict errors printed, but not the non-strict
	// warnings.
	for _, v := range vars {
		if !v.strict {
			continue
		}

		sort := ""
		if v.sort {
			sort = "true"
		}

		fmt.Fprintf(buf, "SOONG_%s := %s\n", v.name, v.value)
		fmt.Fprintf(buf, "$(eval $(call soong-compare-var,%s,%s,my_check_failed := true))\n\n", v.name, sort)
	}

	fmt.Fprint(buf, `
ifneq ($(my_check_failed),false)
  $(error Soong variable check failed)
endif
my_check_failed :=


`)

	for _, v := range vars {
		if v.strict {
			continue
		}

		sort := ""
		if v.sort {
			sort = "true"
		}

		fmt.Fprintf(buf, "SOONG_%s := %s\n", v.name, v.value)
		fmt.Fprintf(buf, "$(eval $(call soong-compare-var,%s,%s))\n\n", v.name, sort)
	}

	fmt.Fprintln(buf, "\nsoong-compare-var :=")

	return buf.Bytes()
}

func (c *makeVarsContext) DeviceConfig() DeviceConfig {
	return DeviceConfig{c.Config().deviceConfig}
}

var ninjaDescaper = strings.NewReplacer("$$", "$")

func (c *makeVarsContext) Eval(ninjaStr string) (string, error) {
	s, err := c.SingletonContext.Eval(c.pctx, ninjaStr)
	if err != nil {
		return "", err
	}
	// SingletonContext.Eval returns an exapnded string that is valid for a ninja file, de-escape $$ to $ for use
	// in a Makefile
	return ninjaDescaper.Replace(s), nil
}

func (c *makeVarsContext) addVariableRaw(name, value string, strict, sort bool) {
	c.vars = append(c.vars, makeVarsVariable{
		name:   name,
		value:  value,
		strict: strict,
		sort:   sort,
	})
}

func (c *makeVarsContext) addVariable(name, ninjaStr string, strict, sort bool) {
	value, err := c.Eval(ninjaStr)
	if err != nil {
		c.SingletonContext.Errorf(err.Error())
	}
	c.addVariableRaw(name, value, strict, sort)
}

func (c *makeVarsContext) Strict(name, ninjaStr string) {
	c.addVariable(name, ninjaStr, true, false)
}
func (c *makeVarsContext) StrictSorted(name, ninjaStr string) {
	c.addVariable(name, ninjaStr, true, true)
}
func (c *makeVarsContext) StrictRaw(name, value string) {
	c.addVariableRaw(name, value, true, false)
}

func (c *makeVarsContext) Check(name, ninjaStr string) {
	c.addVariable(name, ninjaStr, false, false)
}
func (c *makeVarsContext) CheckSorted(name, ninjaStr string) {
	c.addVariable(name, ninjaStr, false, true)
}
func (c *makeVarsContext) CheckRaw(name, value string) {
	c.addVariableRaw(name, value, false, false)
}

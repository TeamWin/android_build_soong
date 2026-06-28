// Copyright (C) 2024 The Android Open Source Project
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

package libbpf_prog

import (
	"fmt"
	"io"
	"runtime"
	"strings"

	"android/soong/android"
	"android/soong/cc"
	"android/soong/genrule"

	"github.com/google/blueprint"
)

type libbpfProgDepType struct {
	blueprint.BaseDependencyTag
}

func init() {
	registerLibbpfProgBuildComponents(android.InitRegistrationContext)
	pctx.Import("android/soong/cc/config")
	pctx.StaticVariable("relPwd", cc.PwdPrefix())
}

var (
	pctx = android.NewPackageContext("android/soong/bpf/libbpf_prog")

	libbpfProgCcRule = pctx.AndroidStaticRule("libbpfProgCcRule",
		blueprint.RuleParams{
			Depfile:     "${out}.d",
			Deps:        blueprint.DepsGCC,
			Command:     "$relPwd $ccCmd --target=bpf -c $cFlags -MD -MF ${out}.d -o $out $in",
			CommandDeps: []string{"$ccCmd"},
		},
		"ccCmd", "cFlags")

	libbpfProgStripRule = pctx.AndroidStaticRule("libbpfProgStripRule",
		blueprint.RuleParams{
			Command: `$stripCmd --strip-unneeded --remove-section=.rel.BTF ` +
				`--remove-section=.rel.BTF.ext --remove-section=.BTF.ext $in -o $out`,
			CommandDeps: []string{"$stripCmd"},
		},
		"stripCmd")

	libbpfProgDepTag = libbpfProgDepType{}
)

func registerLibbpfProgBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("libbpf_defaults", defaultsFactory)
	ctx.RegisterModuleType("libbpf_prog", LibbpfProgFactory)
}

var PrepareForTestWithLibbpfProg = android.GroupFixturePreparers(
	android.FixtureRegisterWithContext(registerLibbpfProgBuildComponents),
	android.FixtureAddFile("libbpf_headers/Foo.h", nil),
	android.FixtureAddFile("libbpf_headers/Android.bp", []byte(`
		genrule {
			name: "libbpf_headers",
			out: ["foo.h",],
		}
	`)),
	genrule.PrepareForTestWithGenRuleBuildComponents,
)

type LibbpfProgProperties struct {
	// source paths to the files.
	Srcs []string `android:"path"`

	// additional cflags that should be used to build the libbpf variant of
	// the C/C++ module.
	Cflags []string `android:"arch_variant"`

	// list of directories relative to the Blueprint file that will
	// be added to the include path using -I
	Local_include_dirs []string `android:"arch_variant"`

	Header_libs []string `android:"arch_variant"`

	// optional subdirectory under which this module is installed into.
	Relative_install_path string
}

type libbpfProg struct {
	android.ModuleBase
	android.DefaultableModuleBase
	properties LibbpfProgProperties
	objs       android.Paths
}

var _ android.ImageInterface = (*libbpfProg)(nil)

func (libbpf *libbpfProg) ImageMutatorBegin(ctx android.ImageInterfaceContext) {}

func (libbpf *libbpfProg) ImageMutatorSupported() bool { return true }

func (libbpf *libbpfProg) VendorVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return false
}

func (libbpf *libbpfProg) ProductVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return false
}

func (libbpf *libbpfProg) CoreVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return true
}

func (libbpf *libbpfProg) RamdiskVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return false
}

func (libbpf *libbpfProg) VendorRamdiskVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return false
}

func (libbpf *libbpfProg) DebugRamdiskVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return false
}

func (libbpf *libbpfProg) RecoveryVariantNeeded(ctx android.ImageInterfaceContext) bool {
	return false
}

func (libbpf *libbpfProg) ExtraImageVariations(ctx android.ImageInterfaceContext) []string {
	return nil
}

func (libbpf *libbpfProg) SetImageVariation(ctx android.ImageInterfaceContext, variation string) {
}

func (libbpf *libbpfProg) DepsMutator(ctx android.BottomUpMutatorContext) {
	ctx.AddDependency(ctx.Module(), libbpfProgDepTag, "libbpf_headers")
	ctx.AddVariationDependencies(nil, cc.HeaderDepTag(), libbpf.properties.Header_libs...)
}

func (libbpf *libbpfProg) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	var cFlagsDeps android.Paths
	cflags := []string{
		"-nostdlibinc",

		// Make paths in deps files relative
		"-no-canonical-prefixes",

		"-O2",
		"-Wall",
		"-Werror",
		"-Wextra",
		// Flag to assist with the transition to libbpf
		"-DENABLE_LIBBPF",
		"-isystem bionic/libc/include",
		"-isystem bionic/libc/kernel/uapi",
		// The architecture doesn't matter here, but asm/types.h is included by linux/types.h.
		"-isystem bionic/libc/kernel/uapi/asm-arm64",
		"-isystem bionic/libc/kernel/android/uapi",
		"-I " + ctx.ModuleDir(),
		"-g", //Libbpf builds require BTF data
	}

	if runtime.GOOS != "darwin" {
		cflags = append(cflags, "-fdebug-prefix-map=/proc/self/cwd=")
	}

	ctx.VisitDirectDeps(func(dep android.Module) {
		depTag := ctx.OtherModuleDependencyTag(dep)
		if depTag == libbpfProgDepTag {
			if genRule, ok := dep.(genrule.SourceFileGenerator); ok {
				cFlagsDeps = append(cFlagsDeps, genRule.GeneratedDeps()...)
				dirs := genRule.GeneratedHeaderDirs()
				for _, dir := range dirs {
					cflags = append(cflags, "-I "+dir.String())
				}
			} else {
				depName := ctx.OtherModuleName(dep)
				ctx.ModuleErrorf("module %q is not a genrule", depName)
			}
		} else if depTag == cc.HeaderDepTag() {
			depExporterInfo, _ := android.OtherModuleProvider(ctx, dep, cc.FlagExporterInfoProvider)
			for _, dir := range depExporterInfo.IncludeDirs {
				cflags = append(cflags, "-I "+dir.String())
			}
		}
	})

	for _, dir := range android.PathsForModuleSrc(ctx, libbpf.properties.Local_include_dirs) {
		cflags = append(cflags, "-I "+dir.String())
	}

	cflags = append(cflags, libbpf.properties.Cflags...)

	srcs := android.PathsForModuleSrc(ctx, libbpf.properties.Srcs)

	for _, src := range srcs {
		if strings.ContainsRune(src.Base(), '_') {
			ctx.ModuleErrorf("invalid character '_' in source name")
		}
		obj := android.ObjPathWithExt(ctx, "unstripped", src, "bpf")

		ctx.Build(pctx, android.BuildParams{
			Rule:      libbpfProgCcRule,
			Input:     src,
			Implicits: cFlagsDeps,
			Output:    obj,
			Args: map[string]string{
				"cFlags": strings.Join(cflags, " "),
				"ccCmd":  "${config.ClangBin}/clang",
			},
		})

		objStripped := android.ObjPathWithExt(ctx, "", src, "bpf")
		ctx.Build(pctx, android.BuildParams{
			Rule:   libbpfProgStripRule,
			Input:  obj,
			Output: objStripped,
			Args: map[string]string{
				"stripCmd": "${config.ClangBin}/llvm-strip",
			},
		})
		libbpf.objs = append(libbpf.objs, objStripped.WithoutRel())
	}

	installDir := android.PathForModuleInstall(ctx, "etc", "bpf")
	if len(libbpf.properties.Relative_install_path) > 0 {
		installDir = installDir.Join(ctx, libbpf.properties.Relative_install_path)
	}
	for _, obj := range libbpf.objs {
		ctx.PackageFile(installDir, obj.Base(), obj)
	}

	ctx.SetOutputFiles(libbpf.objs, "")
}

func (libbpf *libbpfProg) AndroidMk() android.AndroidMkData {
	return android.AndroidMkData{
		Custom: func(w io.Writer, name, prefix, moduleDir string, data android.AndroidMkData) {
			var names []string
			fmt.Fprintln(w)
			fmt.Fprintln(w, "LOCAL_PATH :=", moduleDir)
			fmt.Fprintln(w)
			var localModulePath string
			localModulePath = "LOCAL_MODULE_PATH := $(TARGET_OUT_ETC)/bpf"
			if len(libbpf.properties.Relative_install_path) > 0 {
				localModulePath += "/" + libbpf.properties.Relative_install_path
			}
			for _, obj := range libbpf.objs {
				objName := name + "_" + obj.Base()
				names = append(names, objName)
				fmt.Fprintln(w, "include $(CLEAR_VARS)", " # libbpf.libbpf.obj")
				fmt.Fprintln(w, "LOCAL_MODULE := ", objName)
				fmt.Fprintln(w, "LOCAL_PREBUILT_MODULE_FILE :=", obj.String())
				fmt.Fprintln(w, "LOCAL_MODULE_STEM :=", obj.Base())
				fmt.Fprintln(w, "LOCAL_MODULE_CLASS := ETC")
				fmt.Fprintln(w, localModulePath)
				// AconfigUpdateAndroidMkData may have added elements to Extra.  Process them here.
				for _, extra := range data.Extra {
					extra(w, nil)
				}
				fmt.Fprintln(w, "include $(BUILD_PREBUILT)")
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, "include $(CLEAR_VARS)", " # libbpf.libbpf")
			fmt.Fprintln(w, "LOCAL_MODULE := ", name)
			android.AndroidMkEmitAssignList(w, "LOCAL_REQUIRED_MODULES", names)
			fmt.Fprintln(w, "include $(BUILD_PHONY_PACKAGE)")
		},
	}
}

type Defaults struct {
	android.ModuleBase
	android.DefaultsModuleBase
}

func defaultsFactory() android.Module {
	return DefaultsFactory()
}

func DefaultsFactory(props ...interface{}) android.Module {
	module := &Defaults{}

	module.AddProperties(props...)
	module.AddProperties(&LibbpfProgProperties{})

	android.InitDefaultsModule(module)

	return module
}

func LibbpfProgFactory() android.Module {
	module := &libbpfProg{}

	module.AddProperties(&module.properties)
	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibFirst)
	android.InitDefaultableModule(module)

	return module
}

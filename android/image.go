// Copyright 2019 Google Inc. All rights reserved.
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

type ImageInterfaceContext interface {
	ArchModuleContext

	Module() Module

	ModuleErrorf(fmt string, args ...interface{})
	PropertyErrorf(property, fmt string, args ...interface{})

	DeviceSpecific() bool
	SocSpecific() bool
	ProductSpecific() bool
	SystemExtSpecific() bool
	Platform() bool

	Config() Config
}

// ImageInterface is implemented by modules that need to be split by the imageTransitionMutator.
type ImageInterface interface {
	// ImageMutatorBegin is called before any other method in the ImageInterface.
	ImageMutatorBegin(ctx ImageInterfaceContext)

	// If ImageMutatorSupported returns false then no image variants will be created.
	ImageMutatorSupported() bool

	// VendorVariantNeeded should return true if the module needs a vendor variant (installed on the vendor image).
	VendorVariantNeeded(ctx ImageInterfaceContext) bool

	// ProductVariantNeeded should return true if the module needs a product variant (installed on the product image).
	ProductVariantNeeded(ctx ImageInterfaceContext) bool

	// CoreVariantNeeded should return true if the module needs a core variant (installed on the system image).
	CoreVariantNeeded(ctx ImageInterfaceContext) bool

	// RamdiskVariantNeeded should return true if the module needs a ramdisk variant (installed on the
	// ramdisk partition).
	RamdiskVariantNeeded(ctx ImageInterfaceContext) bool

	// VendorRamdiskVariantNeeded should return true if the module needs a vendor ramdisk variant (installed on the
	// vendor ramdisk partition).
	VendorRamdiskVariantNeeded(ctx ImageInterfaceContext) bool

	// DebugRamdiskVariantNeeded should return true if the module needs a debug ramdisk variant (installed on the
	// debug ramdisk partition: $(PRODUCT_OUT)/debug_ramdisk).
	DebugRamdiskVariantNeeded(ctx ImageInterfaceContext) bool

	// RecoveryVariantNeeded should return true if the module needs a recovery variant (installed on the
	// recovery partition).
	RecoveryVariantNeeded(ctx ImageInterfaceContext) bool

	// ExtraImageVariations should return a list of the additional variations needed for the module.  After the
	// variants are created the SetImageVariation method will be called on each newly created variant with the
	// its variation.
	ExtraImageVariations(ctx ImageInterfaceContext) []string

	// SetImageVariation is called for each newly created image variant. The receiver is the original
	// module, "variation" is the name of the newly created variant. "variation" is set on the receiver.
	SetImageVariation(ctx ImageInterfaceContext, variation string)
}

const (
	// VendorVariation is the variant name used for /vendor code that does not
	// compile against the VNDK.
	VendorVariation string = "vendor"

	// ProductVariation is the variant name used for /product code that does not
	// compile against the VNDK.
	ProductVariation string = "product"

	// CoreVariation is the variant used for framework-private libraries, or
	// SDK libraries. (which framework-private libraries can use), which
	// will be installed to the system image.
	CoreVariation string = ""

	// RecoveryVariation means a module to be installed to recovery image.
	RecoveryVariation string = "recovery"

	// RamdiskVariation means a module to be installed to ramdisk image.
	RamdiskVariation string = "ramdisk"

	// VendorRamdiskVariation means a module to be installed to vendor ramdisk image.
	VendorRamdiskVariation string = "vendor_ramdisk"

	// DebugRamdiskVariation means a module to be installed to debug ramdisk image.
	DebugRamdiskVariation string = "debug_ramdisk"
)

type imageInterfaceContextAdapter struct {
	IncomingTransitionContext
	kind moduleKind
}

var _ ImageInterfaceContext = (*imageInterfaceContextAdapter)(nil)

func (e *imageInterfaceContextAdapter) Platform() bool {
	return e.kind == platformModule
}

func (e *imageInterfaceContextAdapter) DeviceSpecific() bool {
	return e.kind == deviceSpecificModule
}

func (e *imageInterfaceContextAdapter) SocSpecific() bool {
	return e.kind == socSpecificModule
}

func (e *imageInterfaceContextAdapter) ProductSpecific() bool {
	return e.kind == productSpecificModule
}

func (e *imageInterfaceContextAdapter) SystemExtSpecific() bool {
	return e.kind == systemExtSpecificModule
}

// imageMutatorBeginMutator calls ImageMutatorBegin on all modules that may have image variants.
// This happens right before the imageTransitionMutator runs. It's needed to initialize these
// modules so that they return the correct results for all the other ImageInterface methods,
// which the imageTransitionMutator will call. Transition mutators should also not mutate modules
// (except in their Mutate() function), which this method does, so we run it in a separate mutator
// first.
func imageMutatorBeginMutator(ctx BottomUpMutatorContext) {
	if m, ok := ctx.Module().(ImageInterface); ok && ctx.Os() == Android {
		m.ImageMutatorBegin(ctx)
	}
}

// imageTransitionMutator creates variants for modules that implement the ImageInterface that
// allow them to build differently for each partition (recovery, core, vendor, etc.).
type imageTransitionMutator struct{}

func getImageVariations(ctx ImageInterfaceContext) []string {
	var variations []string

	if m, ok := ctx.Module().(ImageInterface); ctx.Os() == Android && ok && m.ImageMutatorSupported() {
		if m.CoreVariantNeeded(ctx) {
			variations = append(variations, CoreVariation)
		}
		if m.RamdiskVariantNeeded(ctx) {
			variations = append(variations, RamdiskVariation)
		}
		if m.VendorRamdiskVariantNeeded(ctx) {
			variations = append(variations, VendorRamdiskVariation)
		}
		if m.DebugRamdiskVariantNeeded(ctx) {
			variations = append(variations, DebugRamdiskVariation)
		}
		if m.RecoveryVariantNeeded(ctx) {
			variations = append(variations, RecoveryVariation)
		}
		if m.VendorVariantNeeded(ctx) {
			variations = append(variations, VendorVariation)
		}
		if m.ProductVariantNeeded(ctx) {
			variations = append(variations, ProductVariation)
		}

		extraVariations := m.ExtraImageVariations(ctx)
		variations = append(variations, extraVariations...)
	}

	if len(variations) == 0 {
		variations = append(variations, "")
	}

	return variations
}

func (imageTransitionMutator) Split(ctx BaseModuleContext) []string {
	return getImageVariations(ctx)
}

func (imageTransitionMutator) OutgoingTransition(ctx OutgoingTransitionContext, sourceVariation string) string {
	return sourceVariation
}

func (imageTransitionMutator) IncomingTransition(ctx IncomingTransitionContext, incomingVariation string) string {
	m, ok := ctx.Module().(ImageInterface)
	if ctx.Os() != Android || !ok || !m.ImageMutatorSupported() {
		return CoreVariation
	}
	variations := getImageVariations(&imageInterfaceContextAdapter{
		IncomingTransitionContext: ctx,
		kind:                      determineModuleKind(ctx.Module().base(), ctx),
	})
	// If there's only 1 possible variation, use that. This is a holdover from when blueprint,
	// when adding dependencies, would use the only variant of a module regardless of its variations
	// if only 1 variant existed.
	if len(variations) == 1 {
		return variations[0]
	}
	return incomingVariation
}

func (imageTransitionMutator) Mutate(ctx BottomUpMutatorContext, variation string) {
	ctx.Module().base().setImageVariation(variation)
	if m, ok := ctx.Module().(ImageInterface); ok {
		m.SetImageVariation(ctx, variation)
	}
}

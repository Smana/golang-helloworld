package settings

import (
	"fmt"
	"hash/fnv"
)

// TailwindCSS 500-weight colors, each used in the palette and in both class maps.
const (
	colorBlue   = "#3B82F6"
	colorGreen  = "#10B981"
	colorAmber  = "#F59E0B"
	colorRed    = "#EF4444"
	colorViolet = "#8B5CF6"
	colorPink   = "#EC4899"
	colorCyan   = "#06B6D4"
	colorOrange = "#F97316"
	colorTeal   = "#14B8A6"
	colorPurple = "#A855F7"
	colorIndigo = "#6366F1"

	blueTagClass = "bg-blue-500 text-white"
)

// TagColorPalette is a curated set of accessible, distinguishable colors
// chosen from TailwindCSS color palette with WCAG AA contrast compliance
var TagColorPalette = []string{
	colorBlue,
	colorGreen,
	colorAmber,
	colorRed,
	colorViolet,
	colorPink,
	colorCyan,
	colorOrange,
	colorTeal,
	colorPurple,
	colorIndigo,
	colorGreen, // emerald-500, the same hex as green-500
}

// TagColorPaletteClasses maps hex colors to TailwindCSS background classes
var TagColorPaletteClasses = map[string]string{
	colorBlue:   blueTagClass,
	colorGreen:  "bg-green-500 text-white",
	colorAmber:  "bg-amber-500 text-white",
	colorRed:    "bg-red-500 text-white",
	colorViolet: "bg-violet-500 text-white",
	colorPink:   "bg-pink-500 text-white",
	colorCyan:   "bg-cyan-500 text-white",
	colorOrange: "bg-orange-500 text-white",
	colorTeal:   "bg-teal-500 text-white",
	colorPurple: "bg-purple-500 text-white",
	colorIndigo: "bg-indigo-500 text-white",
}

// GetTagColor returns a consistent color for a given tag name using FNV-1a hashing
// The same tag name will always produce the same color across all instances
func GetTagColor(tagName string) string {
	if tagName == "" {
		return TagColorPalette[0] // Default to blue for empty tags
	}

	// Use FNV-1a hash for fast, consistent hashing
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(tagName))
	hashValue := hasher.Sum32()

	// Map hash to palette index
	paletteIndex := int(hashValue) % len(TagColorPalette)
	return TagColorPalette[paletteIndex]
}

// GetTagColorClass returns TailwindCSS classes for a tag based on its color
func GetTagColorClass(tagName string) string {
	color := GetTagColor(tagName)
	if class, exists := TagColorPaletteClasses[color]; exists {
		return class
	}
	// Fallback to blue if color not in class map
	return blueTagClass
}

// GetTagStyle returns inline style with background color for a tag
// Useful for HTML rendering where Tailwind classes aren't available
func GetTagStyle(tagName string) string {
	color := GetTagColor(tagName)
	return fmt.Sprintf("background-color: %s; color: white;", color)
}

// GetLightTagColorClass returns a lighter version of the tag color for non-selected states
// Uses Tailwind's 100-weight colors for backgrounds with darker text
var LightTagColorClasses = map[string]string{
	colorBlue:   "bg-blue-100 text-blue-800",
	colorGreen:  "bg-green-100 text-green-800",
	colorAmber:  "bg-amber-100 text-amber-800",
	colorRed:    "bg-red-100 text-red-800",
	colorViolet: "bg-violet-100 text-violet-800",
	colorPink:   "bg-pink-100 text-pink-800",
	colorCyan:   "bg-cyan-100 text-cyan-800",
	colorOrange: "bg-orange-100 text-orange-800",
	colorTeal:   "bg-teal-100 text-teal-800",
	colorPurple: "bg-purple-100 text-purple-800",
	colorIndigo: "bg-indigo-100 text-indigo-800",
}

// GetLightTagColorClass returns light background classes for tag display
func GetLightTagColorClass(tagName string) string {
	color := GetTagColor(tagName)
	if class, exists := LightTagColorClasses[color]; exists {
		return class
	}
	return "bg-gray-100 text-gray-800"
}

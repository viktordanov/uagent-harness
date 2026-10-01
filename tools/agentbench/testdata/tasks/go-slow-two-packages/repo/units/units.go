// Package units converts between measurement units.
package units

// CelsiusToFahrenheit converts a temperature.
func CelsiusToFahrenheit(c float64) float64 { return c*9/5 + 32 }

// FahrenheitToCelsius converts a temperature.
func FahrenheitToCelsius(f float64) float64 { return f - 32*5/9 }

// KmToMiles converts a distance.
func KmToMiles(km float64) float64 { return km / 1.609344 }

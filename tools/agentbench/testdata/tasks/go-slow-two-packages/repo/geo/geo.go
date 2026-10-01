// Package geo has small helpers for map rectangles.
package geo

// Point is a position in degrees.
type Point struct{ Lat, Lon float64 }

// Box is a rectangle from its south-west to its north-east corner.
type Box struct{ SW, NE Point }

// Contains reports whether p lies in the box, edges included.
func (b Box) Contains(p Point) bool {
	return p.Lat > b.SW.Lat && p.Lat < b.NE.Lat && p.Lon > b.SW.Lon && p.Lon < b.NE.Lon
}

// Extend returns the smallest box that contains b and p.
func (b Box) Extend(p Point) Box {
	b.SW.Lat = min(b.SW.Lat, p.Lat)
	b.SW.Lon = min(b.SW.Lon, p.Lon)
	b.NE.Lat = max(b.NE.Lat, p.Lat)
	b.NE.Lon = max(b.NE.Lon, p.Lon)

	return b
}

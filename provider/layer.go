package provider

import (
	"github.com/go-spatial/geom"
	"github.com/go-spatial/tegola/dict"
)

// Layer holds information about a query.
type Layer struct {
	// Name is the name of the Layer as recognized by the provider
	Name string
	// MVTName is the name of the layer to encode into the MVT.
	// this is often used when different provider layers are used
	// at different zoom levels but the MVT layer name is consistent
	MVTName string
}

// Layerer are objects that know about their layers
type Layerer interface {
	// Layers returns information about the various layers the provider supports
	Layers() ([]LayerInfo, error)
	AddLayer(config dict.Dicter) error

	// SRID is the srid of all the points in the layer
	LayerExtent(lryID string) (geom.Extent, error)
	LayerMinZoom(lryID string) int
	LayerMaxZoom(lryID string) int
}

// LayerInfo is the important information about a layer
type LayerInfo interface {
	// Name is the name of the layer
	Name() string
	// GeomType is the geometry type of the layer
	GeomType() geom.Geometry
	// SRID is the srid of all the points in the layer
	SRID() uint64
}

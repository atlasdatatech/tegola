package atlas_test

import (
	"github.com/go-spatial/geom"
	"github.com/go-spatial/tegola/atlas"
	"github.com/go-spatial/tegola/provider/test"
)

var testLayer1 = atlas.Layer{
	ID:              "test-layer-1",
	Name:            "test-layer-name",
	ProviderLayerID: "test-layer-1",
	MinZoom:         4,
	MaxZoom:         9,
	Provider:        &test.TileProvider{},
	GeomType:        geom.Point{},
	DefaultTags: map[string]interface{}{
		"foo": "bar",
	},
}

var testLayer2 = atlas.Layer{
	ID:              "test-layer-2",
	Name:            "test-layer-2-name",
	ProviderLayerID: "test-layer-2-provider-layer-name",
	MinZoom:         10,
	MaxZoom:         20,
	Provider:        &test.TileProvider{},
	GeomType:        geom.LineString{},
	DefaultTags: map[string]interface{}{
		"foo": "bar",
	},
}

var testLayer3 = atlas.Layer{
	ID:              "test-layer-3",
	Name:            "test-layer-3-name",
	ProviderLayerID: "test-layer-3",
	MinZoom:         10,
	MaxZoom:         20,
	Provider:        &test.TileProvider{},
	GeomType:        geom.Point{},
	DefaultTags:     map[string]interface{}{},
}

var testMap = atlas.Map{
	Name:        "test-map",
	Attribution: "test attribution",
	Center:      [3]float64{1.0, 2.0, 3.0},
	Layers: []atlas.Layer{
		testLayer1,
		testLayer2,
		testLayer3,
	},
}

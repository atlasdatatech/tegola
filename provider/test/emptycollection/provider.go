package emptycollection

import (
	"context"

	"github.com/go-spatial/geom"
	"github.com/go-spatial/tegola"
	"github.com/go-spatial/tegola/provider"

	"github.com/go-spatial/tegola/dict"
)

const Name = "emptycollection"

var Count int

func init() {
	provider.Register(provider.TypeStd.Prefix()+Name, NewTileProvider, Cleanup)
}

// NewProvider setups a test provider. there are not currently any config params supported
func NewTileProvider(config dict.Dicter) (provider.Tiler, error) {
	Count++
	return &TileProvider{}, nil
}

// Cleanup cleans up all the test providers.
func Cleanup() { Count = 0 }

type TileProvider struct{}

func (tp *TileProvider) Layers() ([]provider.LayerInfo, error) {
	return []provider.LayerInfo{
		layer{
			name:     "empty_geom_collection",
			geomType: geom.Collection{},
			srid:     tegola.WebMercator,
		},
	}, nil
}

// TilFeatures always returns a feature with a polygon outlining the tile's Extent (not Buffered Extent)
func (tp *TileProvider) TileFeatures(ctx context.Context, layer string, t provider.Tile, fn func(f *provider.Feature) error) error {
	// get tile bounding box
	_, srid := t.Extent()

	debugTileOutline := provider.Feature{
		ID:       0,
		Geometry: geom.Collection{}, // empty geometry collection
		SRID:     srid,
	}

	return fn(&debugTileOutline)
}

// Layer xxx
func (tp *TileProvider) Layer(lyrID string) (provider.LayerInfo, bool) {
	l := layer{
		name:     "empty_geom_collection",
		geomType: geom.Collection{},
		srid:     tegola.WebMercator,
	}
	return l, true
}

//AddLayer xxx
func (tp *TileProvider) AddLayer(config dict.Dicter) error {
	return nil
}

// LayerExtent xxx
func (tp *TileProvider) LayerExtent(lryID string) (geom.Extent, error) {
	ext := geom.Extent{-180.0, -85.05112877980659, 180.0, 85.0511287798066}
	return ext, nil
}

// LayerMinZoom xxx
func (tp *TileProvider) LayerMinZoom(lryID string) int {
	return 0
}

// LayerMaxZoom xxx
func (tp *TileProvider) LayerMaxZoom(lryID string) int {
	return 20
}

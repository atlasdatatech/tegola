// The debug provider returns features that are helpful for debugging a tile
// including a box for the tile edges and a point in the middle of the tile
// with z,x,y values encoded
package debug

import (
	"context"
	"fmt"

	"github.com/go-spatial/geom"
	"github.com/go-spatial/tegola"
	"github.com/go-spatial/tegola/dict"
	"github.com/go-spatial/tegola/provider"
)

const Name = "debug"

const (
	LayerDebugTileOutline = "debug-tile-outline"
	LayerDebugTileCenter  = "debug-tile-center"
)

func init() {
	provider.Register(provider.TypeStd.Prefix()+Name, NewTileProvider, nil)
}

// NewProvider Setups a debug provider. there are not currently any config params supported
func NewTileProvider(config dict.Dicter) (provider.Tiler, error) {
	return &Provider{}, nil
}

// Provider provides the debug provider
type Provider struct{}

// TileFeatures xxx
func (p *Provider) TileFeatures(ctx context.Context, lyrID string, tile provider.Tile, fn func(f *provider.Feature) error) error {

	// get tile bounding box
	ext, srid := tile.Extent()

	switch lyrID {
	case "debug-tile-outline":
		debugTileOutline := provider.Feature{
			ID:       0,
			Geometry: ext.AsPolygon(),
			SRID:     srid,
			Tags: map[string]interface{}{
				"type": "debug_buffer_outline",
			},
		}

		if err := fn(&debugTileOutline); err != nil {
			return err
		}

	case "debug-tile-center":
		xlen := ext.XSpan()
		ylen := ext.YSpan()
		z, x, y := tile.ZXY()

		debugTileCenter := provider.Feature{
			ID: 1,
			Geometry: geom.Point{
				// Minx
				ext.MinX() + (xlen / 2),
				// Miny
				ext.MinY() + (ylen / 2),
			},
			SRID: srid,
			Tags: map[string]interface{}{
				"type": "debug_text",
				"zxy":  fmt.Sprintf("Z:%v, X:%v, Y:%v", z, x, y),
			},
		}

		if err := fn(&debugTileCenter); err != nil {
			return err
		}
	}

	return nil
}

// Layers returns information about the various layers the provider supports
func (p *Provider) Layers() ([]provider.LayerInfo, error) {
	layers := []Layer{
		{
			name:     "debug-tile-outline",
			geomType: geom.Line{},
			srid:     tegola.WebMercator,
		},
		{
			name:     "debug-tile-center",
			geomType: geom.Point{},
			srid:     tegola.WebMercator,
		},
	}

	var ls []provider.LayerInfo

	for i := range layers {
		ls = append(ls, layers[i])
	}

	return ls, nil
}

// Layer returns information about the various layers the provider supports
func (p *Provider) Layer(lyrID string) (provider.LayerInfo, bool) {
	if "debug-tile-center" == lyrID {
		l := Layer{
			id:       "debug-tile-outline",
			name:     "debug-tile-outline",
			geomType: geom.Line{},
			srid:     tegola.WebMercator,
		}
		return l, true
	} else if "debug-tile-outline" == lyrID {
		l := Layer{
			id:       "debug-tile-center",
			name:     "debug-tile-center",
			geomType: geom.Point{},
			srid:     tegola.WebMercator,
		}
		return l, true
	}
	return nil, false
}

// AddLayer xxx
func (p *Provider) AddLayer(config dict.Dicter) error {
	return fmt.Errorf("can not add debug layer")
}

// LayerExtent xxx
func (p *Provider) LayerExtent(lryID string) (geom.Extent, error) {
	ext := geom.Extent{-180.0, -85.05112877980659, 180.0, 85.0511287798066}
	return ext, nil
}

// LayerMinZoom xxx
func (p *Provider) LayerMinZoom(lryID string) int {
	return 0
}

// LayerMaxZoom xxx
func (p *Provider) LayerMaxZoom(lryID string) int {
	return 16
}

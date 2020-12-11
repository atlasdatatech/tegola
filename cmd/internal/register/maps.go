package register

import (
	"html"

	"github.com/go-spatial/geom"
	"github.com/go-spatial/tegola/atlas"
	"github.com/go-spatial/tegola/config"
	"github.com/go-spatial/tegola/provider"
)

func webMercatorMapFromConfigMap(cfg config.Map) (newMap atlas.Map) {
	newMap = atlas.NewWebMercatorMap(string(cfg.Name))
	newMap.Attribution = html.EscapeString(string(cfg.Attribution))

	// convert from env package
	for i, v := range cfg.Center {
		newMap.Center[i] = float64(v)
	}

	if len(cfg.Bounds) == 4 {
		newMap.Bounds = geom.NewExtent(
			[2]float64{float64(cfg.Bounds[0]), float64(cfg.Bounds[1])},
			[2]float64{float64(cfg.Bounds[2]), float64(cfg.Bounds[3])},
		)
	}

	if cfg.TileBuffer != nil {
		newMap.TileBuffer = uint64(*cfg.TileBuffer)
	}
	return newMap

}

func layerInfosFindByID(infos []provider.LayerInfo, lyrID string) provider.LayerInfo {
	if len(infos) == 0 {
		return nil
	}
	for i := range infos {
		if infos[i].ID() == lyrID {
			return infos[i]
		}
	}
	return nil
}

func atlasLayerFromConfigLayer(cfg *config.MapLayer, mapName string, layerProvider provider.Layerer) (layer atlas.Layer, err error) {
	var (
		// providerLayer is primary used for error reporting.
		providerLayer = string(cfg.ProviderLayer)
		ok            bool
	)

	cfg.GetName()
	// read the provider's layer names
	// don't care about the error.
	providerID, plyrID, _ := cfg.ProviderLayerID()
	layerInfos, err := layerProvider.Layers()
	if err != nil {
		return layer, ErrFetchingLayerInfo{
			Provider: providerID,
			Err:      err,
		}
	}
	layerInfo := layerInfosFindByID(layerInfos, plyrID)
	if layerInfo == nil {
		return layer, ErrProviderLayerNotRegistered{
			MapName:       mapName,
			ProviderLayer: providerLayer,
			Provider:      providerID,
		}
	}
	layer.GeomType = layerInfo.GeomType()

	if cfg.DefaultTags != nil {
		if layer.DefaultTags, ok = cfg.DefaultTags.(map[string]interface{}); !ok {
			return layer, ErrDefaultTagsInvalid{
				ProviderLayer: providerLayer,
			}
		}
	}

	// if layerProvider is not a provider.Tiler this will return nil, so
	// no need to check ok, as nil is what we want here.
	layer.Provider, _ = layerProvider.(provider.Tiler)

	layer.ID = string(cfg.ID)
	layer.Name = string(cfg.Name)
	layer.ProviderLayerID = plyrID
	layer.DontSimplify = bool(cfg.DontSimplify)
	layer.DontClip = bool(cfg.DontClip)

	if cfg.MinZoom != nil {
		layer.MinZoom = uint(*cfg.MinZoom)
	}
	if cfg.MaxZoom != nil {
		layer.MaxZoom = uint(*cfg.MaxZoom)
	}
	return layer, nil
}

func selectProvider(prdID string, mapName string, newMap *atlas.Map, providers map[string]provider.TilerUnion) (provider.Layerer, error) {
	if newMap.HasMVTProvider() {
		if newMap.MVTProviderID() != prdID {
			return nil, config.ErrMVTDifferentProviders{
				Original: newMap.MVTProviderID(),
				Current:  prdID,
			}
		}
		return newMap.MVTProvider(), nil
	}
	if prvd, ok := providers[prdID]; ok {
		// Need to see what type of provider we got.
		if prvd.Std != nil {
			return prvd.Std, nil
		}
		if prvd.Mvt == nil {
			return nil, ErrProviderNotFound{prdID}
		}
		if len(newMap.Layers) != 0 {
			return nil, config.ErrMixedProviders{
				Map: string(mapName),
			}
		}
		return newMap.SetMVTProvider(prdID, prvd.Mvt), nil
	}
	return nil, ErrProviderNotFound{prdID}
}

// Maps registers maps with with atlas
func Maps(a *atlas.Atlas, maps []config.Map, providers map[string]provider.TilerUnion) error {

	var (
		layerer provider.Layerer
	)

	// iterate our maps
	for _, m := range maps {
		newMap := webMercatorMapFromConfigMap(m)

		// iterate our layers
		for _, l := range m.Layers {
			prdID, _, err := l.ProviderLayerID()
			if err != nil {
				return ErrProviderLayerInvalid{
					ProviderLayer: string(l.ProviderLayer),
					Map:           string(m.Name),
				}
			}

			// find our layer provider
			layerer, err = selectProvider(prdID, string(m.Name), &newMap, providers)
			if err != nil {
				return err
			}

			layer, err := atlasLayerFromConfigLayer(&l, string(m.Name), layerer)
			if err != nil {
				return err
			}
			newMap.Layers = append(newMap.Layers, layer)
		}
		a.AddMap(newMap)
	}
	return nil
}

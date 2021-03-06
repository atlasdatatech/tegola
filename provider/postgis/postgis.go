package postgis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx"
	"github.com/jackc/pgx/pgtype"

	"github.com/go-spatial/geom"
	"github.com/go-spatial/geom/encoding/wkb"
	"github.com/go-spatial/tegola"
	"github.com/go-spatial/tegola/dict"
	"github.com/go-spatial/tegola/provider"
)

const Name = "postgis"

// Provider provides the postgis data provider.
type Provider struct {
	config pgx.ConnPoolConfig
	pool   *pgx.ConnPool
	// map of layer name and corresponding sql
	layers     map[string]Layer
	srid       uint64
	firstlayer string
}

const (
	// We quote the field and table names to prevent colliding with postgres keywords.
	stdSQL = `SELECT %[1]v FROM %[2]v WHERE "%[3]v" && ` + bboxToken

	// SQL to get the column names, without hitting the information_schema. Though it might be better to hit the information_schema.
	fldsSQL = `SELECT * FROM %[1]v LIMIT 0;`
)

const (
	DefaultPort    = 5432
	DefaultSRID    = tegola.WebMercator
	DefaultMaxConn = 100
	DefaultSSLMode = "disable"
	DefaultSSLKey  = ""
	DefaultSSLCert = ""
)

const (
	ConfigKeyHost        = "host"
	ConfigKeyPort        = "port"
	ConfigKeyDB          = "database"
	ConfigKeyUser        = "user"
	ConfigKeyPassword    = "password"
	ConfigKeySSLMode     = "ssl_mode"
	ConfigKeySSLKey      = "ssl_key"
	ConfigKeySSLCert     = "ssl_cert"
	ConfigKeySSLRootCert = "ssl_root_cert"
	ConfigKeyMaxConn     = "max_connections"
	ConfigKeySRID        = "srid"
	ConfigKeyLayers      = "layers"
	ConfigKeyLayerID     = "id"
	ConfigKeyLayerName   = "name"
	ConfigKeyTablename   = "tablename"
	ConfigKeySQL         = "sql"
	ConfigKeyFields      = "fields"
	ConfigKeyGeomField   = "geometry_fieldname"
	ConfigKeyGeomIDField = "id_fieldname"
	ConfigKeyGeomType    = "geometry_type"
	ConfigKeyLayerType   = "type"
)

// isSelectQuery is a regexp to check if a query starts with `SELECT`,
// case-insensitive and ignoring any preceeding whitespace and SQL comments.
var isSelectQuery = regexp.MustCompile(`(?i)^((\s*)(--.*\n)?)*select`)

// CreateProvider instantiates and returns a new postgis provider or an error.
// The function will validate that the config object looks good before
// trying to create a driver. This Provider supports the following fields
// in the provided map[string]interface{} map:
//
// 	host (string): [Required] postgis database host
// 	port (int): [Required] postgis database port (required)
// 	database (string): [Required] postgis database name
// 	user (string): [Required] postgis database user
// 	password (string): [Required] postgis database password
// 	srid (int): [Optional] The default SRID for the provider. Defaults to WebMercator (3857) but also supports WGS84 (4326)
// 	max_connections : [Optional] The max connections to maintain in the connection pool. Default is 100. 0 means no max.
// 	layers (map[string]struct{})  — This is map of layers keyed by the layer name. supports the following properties
//
// 		name (string): [Required] the name of the layer. This is used to reference this layer from map layers.
// 		tablename (string): [*Required] the name of the database table to query against. Required if sql is not defined.
// 		geometry_fieldname (string): [Optional] the name of the filed which contains the geometry for the feature. defaults to geom
// 		id_fieldname (string): [Optional] the name of the feature id field. defaults to gid
// 		fields ([]string): [Optional] a list of fields to include alongside the feature. Can be used if sql is not defined.
// 		srid (int): [Optional] the SRID of the layer. Supports 3857 (WebMercator) or 4326 (WGS84).
// 		sql (string): [*Required] custom SQL to use use. Required if tablename is not defined. Supports the following tokens:
//
// 			!BBOX! - [Required] will be replaced with the bounding box of the tile before the query is sent to the database.
// 			!ZOOM! - [Optional] will be replaced with the "Z" (zoom) value of the requested tile.
//
func CreateProvider(config dict.Dicter) (*Provider, error) {

	host, err := config.String(ConfigKeyHost, nil)
	if err != nil {
		return nil, err
	}

	db, err := config.String(ConfigKeyDB, nil)
	if err != nil {
		return nil, err
	}

	user, err := config.String(ConfigKeyUser, nil)
	if err != nil {
		return nil, err
	}

	password, err := config.String(ConfigKeyPassword, nil)
	if err != nil {
		return nil, err
	}

	sslmode := DefaultSSLMode
	sslmode, err = config.String(ConfigKeySSLMode, &sslmode)
	if err != nil {
		return nil, err
	}

	sslkey := DefaultSSLKey
	sslkey, err = config.String(ConfigKeySSLKey, &sslkey)
	if err != nil {
		return nil, err
	}

	sslcert := DefaultSSLCert
	sslcert, err = config.String(ConfigKeySSLCert, &sslcert)
	if err != nil {
		return nil, err
	}

	sslrootcert := DefaultSSLCert
	sslrootcert, err = config.String(ConfigKeySSLRootCert, &sslrootcert)
	if err != nil {
		return nil, err
	}

	port := DefaultPort
	if port, err = config.Int(ConfigKeyPort, &port); err != nil {
		return nil, err
	}

	maxcon := DefaultMaxConn
	if maxcon, err = config.Int(ConfigKeyMaxConn, &maxcon); err != nil {
		return nil, err
	}

	srid := DefaultSRID
	if srid, err = config.Int(ConfigKeySRID, &srid); err != nil {
		return nil, err
	}

	connConfig := pgx.ConnConfig{
		Host:     host,
		Port:     uint16(port),
		Database: db,
		User:     user,
		Password: password,
		LogLevel: pgx.LogLevelWarn,
		RuntimeParams: map[string]string{
			"default_transaction_read_only": "TRUE",
			"application_name":              "tegola",
		},
	}

	err = ConfigTLS(sslmode, sslkey, sslcert, sslrootcert, &connConfig)
	if err != nil {
		return nil, err
	}

	p := Provider{
		srid: uint64(srid),
		config: pgx.ConnPoolConfig{
			ConnConfig:     connConfig,
			MaxConnections: int(maxcon),
		},
	}

	if p.pool, err = pgx.NewConnPool(p.config); err != nil {
		return nil, fmt.Errorf("Failed while creating connection pool: %v", err)
	}

	//初始化layers容器
	p.layers = make(map[string]Layer)

	layers, err := config.MapSlice(ConfigKeyLayers)
	if err != nil {
		return nil, err
	}
	for _, layer := range layers {
		p.AddLayer(layer)
	}

	// track the provider so we can clean it up later
	providers = append(providers, p)

	return &p, nil
}

//ConfigTLS derived from github.com/jackc/pgx configTLS (https://github.com/jackc/pgx/blob/master/conn.go)
func ConfigTLS(sslMode string, sslKey string, sslCert string, sslRootCert string, cc *pgx.ConnConfig) error {

	switch sslMode {
	case "disable":
		cc.UseFallbackTLS = false
		cc.TLSConfig = nil
		cc.FallbackTLSConfig = nil
		return nil
	case "allow":
		cc.UseFallbackTLS = true
		cc.FallbackTLSConfig = &tls.Config{InsecureSkipVerify: true}
	case "prefer":
		cc.TLSConfig = &tls.Config{InsecureSkipVerify: true}
		cc.UseFallbackTLS = true
		cc.FallbackTLSConfig = nil
	case "require":
		cc.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	case "verify-ca", "verify-full":
		cc.TLSConfig = &tls.Config{
			ServerName: cc.Host,
		}
	default:
		return ErrInvalidSSLMode(sslMode)
	}

	if sslRootCert != "" {
		caCertPool := x509.NewCertPool()

		caCert, err := ioutil.ReadFile(sslRootCert)
		if err != nil {
			return fmt.Errorf("unable to read CA file (%q): %v", sslRootCert, err)
		}

		if !caCertPool.AppendCertsFromPEM(caCert) {
			return fmt.Errorf("unable to add CA to cert pool")
		}

		cc.TLSConfig.RootCAs = caCertPool
		cc.TLSConfig.ClientCAs = caCertPool
	}

	if (sslCert == "") != (sslKey == "") {
		return fmt.Errorf("both 'sslcert' and 'sslkey' are required")
	} else if sslCert != "" { // we must have both now
		cert, err := tls.LoadX509KeyPair(sslCert, sslKey)
		if err != nil {
			return fmt.Errorf("unable to read cert: %v", err)
		}

		cc.TLSConfig.Certificates = []tls.Certificate{cert}
	}

	return nil
}

// setLayerGeomType sets the geomType field on the layer to one of point,
// linestring, polygon, multipoint, multilinestring, multipolygon or
// geometrycollection
func (p Provider) setLayerGeomType(l *Layer, geomType string) error {
	switch strings.ToLower(geomType) {
	case "point":
		l.geomType = geom.Point{}
	case "linestring":
		l.geomType = geom.LineString{}
	case "polygon":
		l.geomType = geom.Polygon{}
	case "multipoint":
		l.geomType = geom.MultiPoint{}
	case "multilinestring":
		l.geomType = geom.MultiLineString{}
	case "multipolygon":
		l.geomType = geom.MultiPolygon{}
	case "geometrycollection":
		l.geomType = geom.Collection{}
	default:
		return fmt.Errorf("unsupported geometry_type (%v) for layer (%v)", geomType, l.name)
	}
	return nil
}

// inspectLayerGeomType sets the geomType field on the layer by running the SQL
// and reading the geom type in the result set
func (p Provider) inspectLayerGeomType(l *Layer) error {
	var err error
	sql := ""
	// we want to know the geom type instead of returning the geom data so we modify the SQL
	// TODO (arolek): this strategy wont work if remove the requirement of wrapping ST_AsBinary(geom) in the SQL statements.
	//
	// https://github.com/go-spatial/tegola/issues/180
	//
	// case insensitive search
	re := regexp.MustCompile(`(?i)ST_AsBinary`)
	if re.MatchString(l.sql) {
		sql = re.ReplaceAllString(l.sql, "ST_GeometryType")
	} else {
		rgx := regexp.MustCompile(`(?i)ST_AsMVTGeom\((.*?),`)
		rs := rgx.FindStringSubmatch(l.sql)
		if 2 == len(rs) {
			l.geomField = rs[1] //更新geomfield
		}

		var rgx1 = regexp.MustCompile(`(?i)select(.*?)(?i)from`)
		idx := rgx1.FindStringIndex(l.sql)
		if 2 == len(idx) {
			rps := fmt.Sprintf("SELECT ST_GeometryType(%s) FROM", l.geomField)
			sql = strings.Replace(l.sql, l.sql[idx[0]:idx[1]], rps, 1)
		}
	}

	// we only need a single result set to sniff out the geometry type
	sql = fmt.Sprintf("%v LIMIT 1", sql)
	// if a !ZOOM! token exists, all features could be filtered out so we don't have a geometry to inspect it's type.
	// address this by replacing the !ZOOM! token with an ANY statement which includes all zooms
	sql = strings.Replace(sql, "!ZOOM!", "ANY('{0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24}')", 1)
	// we need a tile to run our sql through the replacer
	tile := provider.NewTile(0, 0, 0, 64, tegola.WebMercator)

	// normal replacer
	sql, err = replaceTokens(sql, l, tile, true)
	if err != nil {
		return err
	}

	rows, err := p.pool.Query(sql)
	if err != nil {
		return err
	}
	defer rows.Close()

	// fetch rows FieldDescriptions. this gives us the OID for the data types returned to aid in decoding
	fdescs := rows.FieldDescriptions()
	for rows.Next() {

		vals, err := rows.Values()
		if err != nil {
			return fmt.Errorf("error running SQL: %v ; %v", sql, err)
		}

		// iterate the values returned from our row, sniffing for the geomField or st_geometrytype field name
		for i, v := range vals {
			switch fdescs[i].Name {
			case l.geomField, "st_geometrytype":
				switch v {
				case "ST_Point":
					l.geomType = geom.Point{}
				case "ST_LineString":
					l.geomType = geom.LineString{}
				case "ST_Polygon":
					l.geomType = geom.Polygon{}
				case "ST_MultiPoint":
					l.geomType = geom.MultiPoint{}
				case "ST_MultiLineString":
					l.geomType = geom.MultiLineString{}
				case "ST_MultiPolygon":
					l.geomType = geom.MultiPolygon{}
				case "ST_GeometryCollection":
					l.geomType = geom.Collection{}
				default:
					return fmt.Errorf("layer (%v) returned unsupported geometry type (%v)", l.name, v)
				}
			}
		}
	}

	return rows.Err()
}

// inspectLayerExtent sets the geomType field on the layer by running the SQL
// and reading the box2d the result set
func (p Provider) inspectLayerExtent(l *Layer) (geom.Extent, error) {
	var err error
	ext := geom.Extent{-180.0, -85.05112877980659, 180.0, 85.0511287798066}
	//get geom column
	re := regexp.MustCompile(`(?i)ST_AsBinary`)
	idx := re.FindStringIndex(l.sql)
	if 2 == len(idx) {
		var rgx = regexp.MustCompile(`\((.*?)\)`)
		rs := rgx.FindStringSubmatch(l.sql[idx[1]:])
		if 2 == len(rs) {
			l.geomField = rs[1]
		}
	} else {
		rgx := regexp.MustCompile(`(?i)ST_AsMVTGeom\((.*?),`)
		rs := rgx.FindStringSubmatch(l.sql)
		if 2 == len(rs) {
			l.geomField = rs[1] //更新geomfield
		}
	}
	var sql string
	var rgx1 = regexp.MustCompile(`(?i)select(.*?)(?i)from`)
	idx = rgx1.FindStringIndex(l.sql)
	if 2 == len(idx) {
		rps := fmt.Sprintf("SELECT ST_Extent(%s) FROM", l.geomField)
		sql = strings.Replace(l.sql, l.sql[idx[0]:idx[1]], rps, 1)
	}
	sql = strings.Replace(sql, "!ZOOM!", "ANY('{0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24}')", 1)
	// we need a tile to run our sql through the replacer
	tile := provider.NewTile(0, 0, 0, 64, tegola.WebMercator)
	// normal replacer
	sql, err = replaceTokens(sql, l, tile, true)
	if err != nil {
		return ext, err
	}
	row := p.pool.QueryRow(sql)
	var box string
	err = row.Scan(&box)
	if err != nil {
		return ext, err
	}
	rgxbox := regexp.MustCompile(`(?i)BOX\((.*?)\)`)
	rs := rgxbox.FindStringSubmatch(box)
	if 2 == len(rs) {
		box := strings.Split(rs[1], ",")
		if 2 == len(box) {
			min := strings.Split(box[0], " ")
			max := strings.Split(box[1], " ")
			minx, _ := strconv.ParseFloat(min[0], 64)
			miny, _ := strconv.ParseFloat(min[1], 64)
			maxx, _ := strconv.ParseFloat(max[0], 64)
			maxy, _ := strconv.ParseFloat(max[1], 64)
			return geom.Extent{minx, miny, maxx, maxy}, nil
		}
	}

	return ext, fmt.Errorf("inspect layer(%s) extent error", l.name)
}

// inspectLayerMinZoom inspect the minzoom of the layer
func (p Provider) inspectLayerMinZoom(l *Layer) int {
	minzoom := 0
	bound, err := p.inspectLayerExtent(l)
	if err != nil {
		return minzoom
	}
	return provider.GetBoundZoomLevel(bound, 1920, 1080)
}

// inspectLayerMaxZoom inspect the minzoom of the layer
func (p Provider) inspectLayerMaxZoom(l *Layer) int {
	maxZoom := 16
	bound, err := p.inspectLayerExtent(l)
	if err != nil {
		return maxZoom
	}
	minZoom := provider.GetBoundZoomLevel(bound, 1920, 1080)
	cx := (bound.MinX() + bound.MaxX()) / 2.0
	cy := (bound.MinY() + bound.MaxY()) / 2.0
	z := minZoom
	for ; z < maxZoom; z++ {
		tx := uint(math.Floor((cx + 180.0) / 360.0 * (math.Exp2(float64(z)))))
		ty := uint(math.Floor((1.0 - math.Log(math.Tan(cy*math.Pi/180.0)+1.0/math.Cos(cy*math.Pi/180.0))/math.Pi) / 2.0 * (math.Exp2(float64(z)))))
		//get geom column
		var sql string
		var rgx1 = regexp.MustCompile(`(?i)select(.*?)(?i)from`)
		idx := rgx1.FindStringIndex(l.sql)
		if 2 == len(idx) {
			rps := "SELECT COUNT(*) FROM"
			sql = strings.Replace(l.sql, l.sql[idx[0]:idx[1]], rps, 1)
		}
		sql = strings.Replace(sql, "!ZOOM!", "ANY('{0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24}')", 1)
		// we need a tile to run our sql through the replacer
		tile := provider.NewTile(uint(z), tx, ty, 64, tegola.WebMercator)
		// normal replacer
		sql, err = replaceTokens(sql, l, tile, true)
		if err != nil {
			return maxZoom
		}
		row := p.pool.QueryRow(sql)
		var cnt int
		err = row.Scan(&cnt)
		if err != nil {
			return maxZoom
		}
		if cnt < 1024 {
			return z
		}
	}
	return maxZoom
}

// Layer fetches an individual layer from the provider, if it's configured
// if no name is provider, the first layer is returned
func (p *Provider) Layer(lryID string) (provider.LayerInfo, bool) {
	layer, ok := p.layers[lryID]
	return layer, ok
}

// Layers returns meta data about the various layers which are configured with the provider
func (p *Provider) Layers() ([]provider.LayerInfo, error) {
	var ls []provider.LayerInfo
	for i := range p.layers {
		ls = append(ls, p.layers[i])
	}
	return ls, nil
}

// TileFeatures adheres to the provider.Tiler interface
func (p *Provider) TileFeatures(ctx context.Context, lyrID string, tile provider.Tile, fn func(f *provider.Feature) error) error {
	// fetch the provider layer
	// plyr, ok := p.Layer(layerid)

	plyr, ok := p.layers[lyrID]
	if !ok {
		return ErrLayerNotFound{lyrID}
	}

	sql, err := replaceTokens(plyr.sql, &plyr, tile, true)
	if err != nil {
		return fmt.Errorf("error replacing layer tokens for layer (%v) SQL (%v): %v", lyrID, sql, err)
	}

	if debugExecuteSQL {
		log.Printf("TEGOLA_SQL_DEBUG:EXECUTE_SQL for layer (%v): %v", lyrID, sql)
	}

	// context check
	if err := ctx.Err(); err != nil {
		return err
	}

	rows, err := p.pool.Query(sql)
	if err != nil {
		return fmt.Errorf("error running layer (%v) SQL (%v): %v", lyrID, sql, err)
	}
	defer rows.Close()

	// fetch rows FieldDescriptions. this gives us the OID for the data types returned to aid in decoding
	fdescs := rows.FieldDescriptions()

	// loop our field descriptions looking for the geometry field
	var geomFieldFound bool
	for i := range fdescs {
		if fdescs[i].Name == plyr.GeomFieldName() {
			geomFieldFound = true
			break
		}
	}
	if !geomFieldFound {
		return ErrGeomFieldNotFound{
			GeomFieldName: plyr.GeomFieldName(),
			LayerName:     plyr.Name(),
		}
	}

	reportedLayerFieldName := ""
	for rows.Next() {
		// context check
		if err := ctx.Err(); err != nil {
			return err
		}

		// fetch row values
		vals, err := rows.Values()
		if err != nil {
			return fmt.Errorf("error running layer (%v) SQL (%v): %v", lyrID, sql, err)
		}

		gid, geobytes, tags, err := decipherFields(ctx, plyr.GeomFieldName(), plyr.IDFieldName(), fdescs, vals)
		if err != nil {
			switch err {
			case context.Canceled:
				return err
			default:
				return fmt.Errorf("for layer (%v) %v", plyr.Name(), err)
			}
		}

		// check that we have geometry data. if not, skip the feature
		if len(geobytes) == 0 {
			continue
		}

		// decode our WKB
		geometry, err := wkb.DecodeBytes(geobytes)
		if err != nil {
			switch err.(type) {
			case wkb.ErrUnknownGeometryType:
				rplfn := lyrID + ":" + plyr.GeomFieldName()
				// Only report to the log once. This is to prevent the logs from filling up if there are many geometries in the layer
				if reportedLayerFieldName == "" || reportedLayerFieldName == rplfn {
					reportedLayerFieldName = rplfn
					log.Printf("[WARNING] Ignoring unsupported geometry in layer (%v). Only basic 2D geometry type are supported. Try using `ST_Force2D(%v)`.", lyrID, plyr.GeomFieldName())
				}
				continue
			default:
				return fmt.Errorf("unable to decode layer (%v) geometry field (%v) into wkb where (%v = %v): %v", lyrID, plyr.GeomFieldName(), plyr.IDFieldName(), gid, err)
			}
		}

		feature := provider.Feature{
			ID:       gid,
			Geometry: geometry,
			SRID:     plyr.SRID(),
			Tags:     tags,
		}

		// pass the feature to the provided callback
		if err = fn(&feature); err != nil {
			return err
		}
	}

	return rows.Err()
}

// MVTForLayers xxx
func (p *Provider) MVTForLayers(ctx context.Context, tile provider.Tile, layers []provider.Layer) ([]byte, error) {
	var (
		err  error
		sqls = make([]string, 0, len(layers))
	)

	for i := range layers {
		if debug {
			log.Printf("looking for layer: %v", layers[i])
		}

		l, ok := p.layers[layers[i].ID]
		if !ok {
			// Should we be erroring here, or have a flag so that we don't
			// spam the user?
			log.Printf("provider layer not found %v", layers[i].ID)
		}
		if debugLayerSQL {
			log.Printf("SQL for Layer(%v):\n%v\n", l.Name(), l.sql)
		}
		sql, err := replaceTokens(l.sql, &l, tile, false)
		if err != nil {
			return nil, err
		}
		// fmt.Println(sql)
		// ref: https://postgis.net/docs/ST_AsMVT.html
		// bytea ST_AsMVT(anyelement row, text name, integer extent, text geom_name, text feature_id_name)
		asmvt := fmt.Sprintf(
			`(SELECT ST_AsMVT(q,'%s',%d,'%s') AS data FROM (%s) AS q)`,
			layers[i].MVTName,
			tegola.DefaultExtent,
			l.GeomFieldName(),
			sql,
		)
		if "" != l.IDFieldName() {
			asmvt = fmt.Sprintf(
				`(SELECT ST_AsMVT(q,'%s',%d,'%s','%s') AS data FROM (%s) AS q)`,
				layers[i].MVTName,
				tegola.DefaultExtent,
				l.GeomFieldName(),
				l.IDFieldName(),
				sql,
			)
		}
		sqls = append(sqls, asmvt)
	}
	subsqls := strings.Join(sqls, "||")
	fsql := fmt.Sprintf(`SELECT (%s) AS data`, subsqls)
	// fmt.Println(fsql)
	var data pgtype.Bytea
	if debugExecuteSQL {
		log.Printf("%s:%s: %v", EnvSQLDebugName, EnvSQLDebugExecute, fsql)
	}
	err = p.pool.QueryRow(fsql).Scan(&data)
	if debugExecuteSQL {
		log.Printf("%s:%s: %v", EnvSQLDebugName, EnvSQLDebugExecute, fsql)
		if err != nil {
			log.Printf("%s:%s: returned error %v", EnvSQLDebugName, EnvSQLDebugExecute, err)
		} else {
			log.Printf("%s:%s: returned %v bytes", EnvSQLDebugName, EnvSQLDebugExecute, len(data.Bytes))
		}
	}

	// data may have garbage in it.
	if err != nil {
		return []byte{}, err
	}
	return data.Bytes, nil
}

// Close will close the Provider's database connectio
func (p *Provider) Close() { p.pool.Close() }

// reference to all instantiated providers
var providers []Provider

// Cleanup will close all database connections and destroy all previously instantiated Provider instances
func Cleanup() {
	if len(providers) > 0 {
		log.Printf("cleaning up postgis providers")
	}

	for i := range providers {
		providers[i].Close()
	}

	providers = make([]Provider, 0)
}

// AddLayer 添加驱动层
func (p *Provider) AddLayer(layer dict.Dicter) error {

	lid, err := layer.String(ConfigKeyLayerID, nil)
	if err != nil {
		return fmt.Errorf("AddLayer, we got the following error trying to get the layer's name field: %v", err)
	}

	if j, ok := p.layers[lid]; ok {
		return fmt.Errorf("%v layer name is duplicated", lid, j)
	}

	if len(p.layers) == 0 {
		p.firstlayer = lid
	}

	lname, err := layer.String(ConfigKeyLayerName, nil)
	if err != nil {
		return fmt.Errorf("AddLayer, we got the following error trying to get the layer's name field: %v", err)
	}

	fields, err := layer.StringSlice(ConfigKeyFields)
	if err != nil {
		return fmt.Errorf("for layer (%v) %v field had the following error: %v", lid, ConfigKeyFields, err)
	}

	geomfld := "geom"
	geomfld, err = layer.String(ConfigKeyGeomField, &geomfld)
	if err != nil {
		return fmt.Errorf("for layer (%v) : %v", lid, err)
	}

	idfld := ""
	idfld, err = layer.String(ConfigKeyGeomIDField, &idfld)
	if err != nil {
		return fmt.Errorf("for layer (%v) : %v", lid, err)
	}
	if idfld == geomfld {
		return fmt.Errorf("for layer %v: %v (%v) and %v field (%v) is the same", lid, ConfigKeyGeomField, geomfld, ConfigKeyGeomIDField, idfld)
	}

	geomType := ""
	geomType, err = layer.String(ConfigKeyGeomType, &geomType)
	if err != nil {
		return fmt.Errorf("for layer  %v : %v", lid, err)
	}

	var tblName string
	tblName, err = layer.String(ConfigKeyTablename, &lname)
	if err != nil {
		return fmt.Errorf("forlayer (%v) %v has an error: %v", lid, ConfigKeyTablename, err)
	}

	var sql string
	sql, err = layer.String(ConfigKeySQL, &sql)
	if err != nil {
		return fmt.Errorf("for layer (%v) %v has an error: %v", lid, ConfigKeySQL, err)
	}

	if tblName != lname && sql != "" {
		log.Printf("both %v and %v field are specified for layer (%v), using only %[2]v field.", ConfigKeyTablename, ConfigKeySQL, lid)
	}

	var lsrid = int(p.srid)
	if lsrid, err = layer.Int(ConfigKeySRID, &lsrid); err != nil {
		return err
	}

	l := Layer{
		id:        lid,
		name:      lname,
		idField:   idfld,
		geomField: geomfld,
		srid:      uint64(lsrid),
	}

	if sql != "" && !isSelectQuery.MatchString(sql) {
		// if it is not a SELECT query, then we assume we have a sub-query
		// (`(select ...) as foo`) which we can handle like a tablename
		tblName = sql
		sql = ""
	}

	if sql != "" {
		// convert !BOX! (MapServer) and !bbox! (Mapnik) to !BBOX! for compatibility
		sql := strings.Replace(strings.Replace(sql, "!BOX!", "!BBOX!", -1), "!bbox!", "!BBOX!", -1)
		// make sure that the sql has a !BBOX! token
		if !strings.Contains(sql, bboxToken) {
			return fmt.Errorf("SQL for layer (%v) is missing required token: %v", lid, bboxToken)
		}
		if !strings.Contains(sql, "*") {
			if !strings.Contains(sql, geomfld) {
				return fmt.Errorf("SQL for layer (%v) does not contain the geometry field: %v", lid, geomfld)
			}
			if !strings.Contains(sql, idfld) {
				return fmt.Errorf("SQL for layer (%v) does not contain the id field for the geometry: %v", lid, idfld)
			}
		}

		l.sql = sql
	} else {
		// Tablename and Fields will be used to build the query.
		// We need to do some work. We need to check to see Fields contains the geom and gid fields
		// and if not add them to the list. If Fields list is empty/nil we will use '*' for the field list.
		//默认mvt
		layerType, _ := layer.String(ConfigKeyLayerType, nil)
		if layerType == "postgis" {
			l.sql, err = genSQL(&l, p.pool, tblName, fields, true)
		} else {
			l.sql, err = genMvtSQL(&l, p.pool, tblName, fields, true)
		}
		if err != nil {
			return fmt.Errorf("could not generate sql, for layer(%v): %v", lid, err)
		}
	}

	if strings.Contains(os.Getenv("TEGOLA_SQL_DEBUG"), "LAYER_SQL") {
		log.Printf("SQL for Layer(%v):\n%v\n", lid, l.sql)
	}

	// set the layer geom type
	if geomType != "" {
		if err = p.setLayerGeomType(&l, geomType); err != nil {
			return fmt.Errorf("error fetching geometry type for layer (%v): %v", lid, err)
		}
	} else {
		if err = p.inspectLayerGeomType(&l); err != nil {
			return fmt.Errorf("error fetching geometry type for layer (%v): %v", lid, err)
		}
	}

	p.layers[lid] = l
	return nil
}

// LayerExtent xxx
func (p *Provider) LayerExtent(lyrID string) (geom.Extent, error) {
	ext := geom.Extent{-180.0, -85.05112877980659, 180.0, 85.0511287798066}
	layer, ok := p.layers[lyrID]
	if !ok {
		return ext, fmt.Errorf("layer id not exist")
	}
	return p.inspectLayerExtent(&layer)
}

// LayerMinZoom xxx
func (p *Provider) LayerMinZoom(lryID string) int {
	minZoom := 0
	layer, ok := p.layers[lryID]
	if !ok {
		return minZoom
	}
	return p.inspectLayerMinZoom(&layer)
}

// LayerMaxZoom xxx
func (p *Provider) LayerMaxZoom(lryID string) int {
	maxZoom := 16
	layer, ok := p.layers[lryID]
	if !ok {
		return maxZoom
	}
	return p.inspectLayerMaxZoom(&layer)
}

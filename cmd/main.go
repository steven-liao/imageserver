package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"github.com/disintegration/gift"
	"github.com/pierrre/imageserver"
	imageserver_cache "github.com/pierrre/imageserver/cache"
	imageserver_cache_memory "github.com/pierrre/imageserver/cache/memory"
	imageserver_http "github.com/pierrre/imageserver/http"
	imageserver_http_crop "github.com/pierrre/imageserver/http/crop"
	imageserver_http_gamma "github.com/pierrre/imageserver/http/gamma"
	imageserver_http_gift "github.com/pierrre/imageserver/http/gift"
	imageserver_http_image "github.com/pierrre/imageserver/http/image"
	imageserver_image "github.com/pierrre/imageserver/image"
	_ "github.com/pierrre/imageserver/image/bmp"
	imageserver_image_crop "github.com/pierrre/imageserver/image/crop"
	imageserver_image_gamma "github.com/pierrre/imageserver/image/gamma"
	imageserver_image_gif "github.com/pierrre/imageserver/image/gif"
	imageserver_image_gift "github.com/pierrre/imageserver/image/gift"
	_ "github.com/pierrre/imageserver/image/jpeg"
	_ "github.com/pierrre/imageserver/image/png"
	_ "github.com/pierrre/imageserver/image/tiff"
	"image"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

var (
	flagHTTP  = ":8080"
	flagCache = int64(128 * (1 << 20))
	dirName   = "upload"
	maxWidth  = int(1024 * 2)
	maxHeight = int(1024 * 2)
)

func main() {
	parseFlags()
	startHTTPServer()
}

func parseFlags() {
	flag.StringVar(&flagHTTP, "http", flagHTTP, "监听端口")
	flag.Int64Var(&flagCache, "cache", flagCache, "缓存大小")
	flag.StringVar(&dirName, "dir", dirName, "图片目录")
	flag.IntVar(&maxWidth, "maxw", maxWidth, "最大宽度")
	flag.IntVar(&maxHeight, "maxh", maxHeight, "最大高度")
	flag.Parse()
}

func startHTTPServer() {
	fmt.Printf("\n启动服务器：http://127.0.0.1" + flagHTTP)
	err := http.ListenAndServe(flagHTTP, newHTTPHandler())
	if err != nil {
		panic(err)
	}
}

func newHTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.StripPrefix("/", newImageHTTPHandler()))
	mux.Handle("/favicon.ico", http.NotFoundHandler())

	return mux
}

func newImageHTTPHandler() http.Handler {
	var handler http.Handler = &imageserver_http.Handler{
		Parser: imageserver_http.ListParser([]imageserver_http.Parser{
			&imageserver_http.SourcePathParser{},
			&imageserver_http_crop.Parser{},
			&imageserver_http_gift.RotateParser{},
			&imageserver_http_gift.ResizeParser{},
			&imageserver_http_image.FormatParser{},
			&imageserver_http_image.QualityParser{},
			&imageserver_http_gamma.CorrectionParser{},
		}),
		Server:   newServer(),
		ETagFunc: imageserver_http.NewParamsHashETagFunc(sha256.New),
	}
	handler = &imageserver_http.ExpiresHandler{
		Handler: handler,
		Expires: 7 * 24 * time.Hour,
	}
	handler = &imageserver_http.CacheControlPublicHandler{
		Handler: handler,
	}
	return handler
}

func newServer() imageserver.Server {
	srv := getImgServer()
	srv = newServerImage(srv)
	srv = newServerLimit(srv)
	srv = newServerCacheMemory(srv)
	return srv
}

func newServerImage(srv imageserver.Server) imageserver.Server {
	basicHdr := &imageserver_image.Handler{
		Processor: imageserver_image_gamma.NewCorrectionProcessor(
			imageserver_image.ListProcessor([]imageserver_image.Processor{
				&imageserver_image_crop.Processor{},
				&imageserver_image_gift.RotateProcessor{
					DefaultInterpolation: gift.CubicInterpolation,
				},
				&imageserver_image_gift.ResizeProcessor{
					DefaultResampling: gift.LanczosResampling,
					MaxWidth:          maxWidth,
					MaxHeight:         maxHeight,
				},
			}),
			true,
		),
	}
	gifHdr := &imageserver_image_gif.FallbackHandler{
		Handler: &imageserver_image_gif.Handler{
			Processor: &imageserver_image_gif.SimpleProcessor{
				Processor: imageserver_image.ListProcessor([]imageserver_image.Processor{
					&imageserver_image_crop.Processor{},
					&imageserver_image_gift.RotateProcessor{
						DefaultInterpolation: gift.NearestNeighborInterpolation,
					},
					&imageserver_image_gift.ResizeProcessor{
						DefaultResampling: gift.NearestNeighborResampling,
						MaxWidth:          maxWidth,
						MaxHeight:         maxHeight,
					},
				}),
			},
		},
		Fallback: basicHdr,
	}
	return &imageserver.HandlerServer{
		Server:  srv,
		Handler: gifHdr,
	}
}

func newServerLimit(srv imageserver.Server) imageserver.Server {
	return imageserver.NewLimitServer(srv, runtime.GOMAXPROCS(0)*2)
}

func newServerCacheMemory(srv imageserver.Server) imageserver.Server {
	if flagCache <= 0 {
		return srv
	}
	return &imageserver_cache.Server{
		Server:       srv,
		Cache:        imageserver_cache_memory.New(flagCache),
		KeyGenerator: imageserver_cache.NewParamsHashKeyGenerator(sha256.New),
	}
}

//根据请求返回本地图片
func getImgServer() imageserver.Server {
	return imageserver.Server(imageserver.ServerFunc(func(params imageserver.Params) (*imageserver.Image, error) {
		source, err := params.GetString("source")
		if err != nil {
			return nil, err
		}
		filePath := filepath.Join(dirName, source)
		data, err := os.Open(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, &imageserver.ImageError{
					Message: "404",
				}
			}
			panic(err)
		}
		_, format, err := image.DecodeConfig(data)
		if err != nil {
			panic(err)
		}
		b, err := ioutil.ReadFile(filePath)
		if err != nil {
			panic(err)
		}

		im := &imageserver.Image{
			Format: format,
			Data:   b,
		}
		return im, nil
	}))
}

package web

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type measurement struct {
	name    string
	measure Measure
}

type measureContext struct {
}

func middleware1(ctx *measureContext, w ResponseWriter, r *Request, next NextMiddlewareFunc) {
	next(w, r)
}

func middleware2(ctx *measureContext, w ResponseWriter, r *Request, next NextMiddlewareFunc) {
	next(w, r)
}

func TestMeasure(t *testing.T) {
	router := New(measureContext{})

	var measurements []measurement
	router.MeasureMiddlewares(0, func(ctx *measureContext, name string, measure Measure, start, end time.Time) {
		measurements = append(measurements, measurement{
			name:    name,
			measure: measure,
		})
	})
	router.Middleware(middleware1)
	router.Middleware(middleware2)

	rw, req := newTestRequest("GET", "/this_path_doesnt_exist")
	router.ServeHTTP(rw, req)
	assert.Equal(t, []measurement{
		{
			name:    "github.com/joomcode/gocraft-web.middleware1",
			measure: MeasureBefore,
		},
		{
			name:    "github.com/joomcode/gocraft-web.middleware2",
			measure: MeasureBefore,
		},
		{
			name:    "github.com/joomcode/gocraft-web.middleware2",
			measure: MeasureAfter,
		},
		{
			name:    "github.com/joomcode/gocraft-web.middleware1",
			measure: MeasureAfter,
		},
	}, measurements)
}

func TestMeasureWithLargeThreshold(t *testing.T) {
	router := New(measureContext{})

	var measurements []measurement
	router.MeasureMiddlewares(time.Hour, func(ctx *measureContext, name string, measure Measure, start, end time.Time) {
		measurements = append(measurements, measurement{
			name:    name,
			measure: measure,
		})
	})
	router.Middleware(middleware1)
	router.Middleware(middleware2)

	rw, req := newTestRequest("GET", "/this_path_doesnt_exist")
	router.ServeHTTP(rw, req)
	assert.Equal(t, []measurement(nil), measurements)
}

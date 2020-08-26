package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateRoutePath(t *testing.T) {
	type MyContext struct{}

	r := New(MyContext{})
	r2 := r.Subrouter(MyContext{}, "/hello")
	r3 := r2.Subrouter(MyContext{}, "/babe")
	r3.Post("/and/goodbye", func(ctx *MyContext, rw ResponseWriter, r *Request) {})
	r3.Post("/and/goodbye/:id", func(ctx *MyContext, rw ResponseWriter, r *Request) {})

	for _, cas := range []struct {
		path          string
		expectedRoute string
	}{
		{"/hello/babe/and/goodbye", "/hello/babe/and/goodbye"},
		{"/hello/babe/and/goodbye/", "/hello/babe/and/goodbye"},
		{"/hello/babe/and/goodbye/:id", "/hello/babe/and/goodbye/:id"},
		{"/hello/babe/and/goodbye/1", "/hello/babe/and/goodbye/:id"},
		{"/hello/babe/and/goodbye/:id/", "/hello/babe/and/goodbye/:id"},
		{"/hello/babe/and/goodbye/:type", "/hello/babe/and/goodbye/:id"},
		{"/hello/babe/and/goodbye/:id/1", ""},
		{"/hello/babe/and/goodbyee/1", ""},
		{"", ""},
	} {
		assert.Equal(t, cas.expectedRoute, CalculateRoutePath(r, "POST", cas.path), "For path %s", cas.path)
		assert.Equal(t, cas.expectedRoute, CalculateRoutePath(r3, "POST", cas.path), "For path %s", cas.path)
	}

	assert.Panics(t, func() {
		CalculateRoutePath(r, "unknown http method", "/hello/babe/and/goodbye/1")
	})
}

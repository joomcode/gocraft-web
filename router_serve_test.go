package web

import (
	"io/ioutil"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestEncodedRoutes(t *testing.T) {
	type MyContext struct{}

	router := New(MyContext{})
	router.Get("/testPath/:id/suffix", func(ctx *MyContext, rw ResponseWriter, req *Request) {
		_, _ = rw.Write([]byte(req.PathParams["id"]))
	})
	router.Get("/testPath/:id/%2F", func(ctx *MyContext, rw ResponseWriter, req *Request) {
		_, _ = rw.Write([]byte("two-f"))
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	c := srv.Client()

	getResponseBodyString := func(path string) string {
		resp, err := c.Get(srv.URL + path)
		require.NoError(t, err)
		defer resp.Body.Close()
		all, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		return string(all)
	}

	assert.Equal(t, "id", getResponseBodyString("/testPath/id/suffix"))
	assert.Equal(t, "id", getResponseBodyString("/testPath/id/suffix?params=%2F"))
	assert.Equal(t, "two-f", getResponseBodyString("/testPath/id/%2F"))
	assert.Equal(t, "id with spaces", getResponseBodyString("/testPath/id%20with%20spaces/suffix"))
	assert.Equal(t, "id/with/slashes", getResponseBodyString("/testPath/id%2Fwith%2Fslashes/suffix"))
}

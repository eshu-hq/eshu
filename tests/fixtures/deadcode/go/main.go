package main

import (
	"net/http"
	"reflect"
)

type localHandler interface {
	Handle()
}

type localWorker struct{}

func main() {
	directReference()
	http.HandleFunc("/readyz", serveHTTP)
	callbacks := []func(){functionValueRoot}
	worker := localWorker{}
	methodCallbacks := []func(){worker.run}
	var handler localHandler = worker
	handler.Handle()
	_ = callbacks
	_ = methodCallbacks
	_ = reflect.ValueOf(worker).MethodByName("reflectiveAmbiguous")
}

func unusedHelper() {}

func directReference() {}

func PublicAPI() {}

func serveHTTP(w http.ResponseWriter, r *http.Request) {}

func functionValueRoot() {}

func (localWorker) run() {}

func (localWorker) Handle() {}

func (localWorker) reflectiveAmbiguous() {}

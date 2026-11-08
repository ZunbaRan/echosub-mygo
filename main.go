package main

import (
	"log"

	"github.com/egoist/mygo"
)

func main() {
	app := NewAppState()
	mw := newMainWindow(app)
	mygo.Bind(mw.bridge)
	mygo.App.WhenReady(func() {
		mw.show()
		mw.setupMenus()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

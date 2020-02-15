// Structure of data for all state changes
package erdomain

import (
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/eventhorizon/pkg/ehevent"
)

var Types = ehevent.Allocators{
	"AppUpdated": func() ehevent.Event { return &AppUpdated{} },
	"AppDeleted": func() ehevent.Event { return &AppDeleted{} },
}

// ------

type AppUpdated struct {
	meta        ehevent.EventMeta
	Application erconfig.Application
}

func (e *AppUpdated) MetaType() string         { return "AppUpdated" }
func (e *AppUpdated) Meta() *ehevent.EventMeta { return &e.meta }

func NewAppUpdated(
	app erconfig.Application,
	meta ehevent.EventMeta,
) *AppUpdated {
	return &AppUpdated{
		meta:        meta,
		Application: app,
	}
}

// ------

type AppDeleted struct {
	meta ehevent.EventMeta
	Id   string
}

func (e *AppDeleted) MetaType() string         { return "AppDeleted" }
func (e *AppDeleted) Meta() *ehevent.EventMeta { return &e.meta }

func NewAppDeleted(
	id string,
	meta ehevent.EventMeta,
) *AppDeleted {
	return &AppDeleted{
		meta: meta,
		Id:   id,
	}
}

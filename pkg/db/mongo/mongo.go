package mongo

import (
	"vendor.lib/tng/tng-lib/db/mgo"
)

type Mongo struct {
	mgo.Mongo
	Collection string
}
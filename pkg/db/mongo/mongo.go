package mongo

import (
	"vendor.lib/tng/tng-lib/db/mgo"
)

type Mongo struct {
	mgo.Mongo
	Collection string
}

func (ss *Mongo) FindUser(userId string, ctx context.Context) (model.User, error){
	var user model.User
	collection := ss.Database(ctx, bson.M{"userId":userId}).Decode(&user)
	if err != nil{
		return user, nil
	}
	return user, nil
}
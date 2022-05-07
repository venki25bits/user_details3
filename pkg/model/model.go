package model

type User struct{
	id string `bson:"id" json:"id"`
	firstName string `bson:"firstName" json:"firstName"`
	lastName string `bson:"lastName" json:"lastName"`
	userName string `bson:"userName" json:"userName"`
	emailId string `bson:"emailId" json:"emailId"`
	password string `bson:"password" json:"password"` 
	contact string `bson:"contact" json:"contact"`
}

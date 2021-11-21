module github.com/serg666/accstorageapp

go 1.15

require (
	github.com/google/uuid v1.3.0
	github.com/gorilla/sessions v1.2.1
	github.com/hyperledger/fabric-sdk-go v1.0.0
	github.com/serg666/accountstorage v0.0.0-00010101000000-000000000000
	github.com/shomali11/util v0.0.0-20200329021417-91c54758c87b
)

replace github.com/serg666/accountstorage => ../accountstorage

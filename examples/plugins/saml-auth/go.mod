module wave.dev/example/saml-auth

go 1.23

require (
	wave.dev/sdk v0.0.0
	github.com/crewjam/saml v0.4.14
)

require (
	github.com/beevik/etree v1.1.0 // indirect
	github.com/crewjam/httperr v0.2.0 // indirect
	github.com/golang-jwt/jwt/v4 v4.4.3 // indirect
	github.com/jonboulle/clockwork v0.2.2 // indirect
	github.com/mattermost/xml-roundtrip-validator v0.1.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/russellhaering/goxmldsig v1.3.0 // indirect
	golang.org/x/crypto v0.14.0 // indirect
)

replace wave.dev/sdk => ../../../sdk/wave

module github.com/zsiec/prism

go 1.24.3

replace github.com/quic-go/quic-go => github.com/zsiec/quic-go v0.59.1-bbr

require (
	github.com/quic-go/quic-go v0.59.0
	github.com/quic-go/webtransport-go v0.10.0
	github.com/zsiec/ccx v0.2.0
	github.com/zsiec/srtgo v0.2.4
	golang.org/x/sync v0.17.0
)

require (
	github.com/dunglas/httpsfv v1.1.0 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	golang.org/x/crypto v0.42.0 // indirect
	golang.org/x/net v0.44.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
	golang.org/x/text v0.30.0 // indirect
)

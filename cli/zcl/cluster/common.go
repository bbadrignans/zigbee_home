package cluster

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

const (
	Server          Side = 1 << iota
	Client          Side = 1 << iota
	ClientAndServer Side = Server | Client
)

var _ yaml.Unmarshaler = (*Side)(nil)

type Side uint8

type Provider interface {
	Clusters() Clusters
}

type Cluster interface {
	ID() ID
	CAttrType() string
	CVarName() string
	// ReportAttrCount returns how many attributes are reportable
	ReportAttrCount() int
	// Side tells if the cluster is client and/or server. ZCL 1.3.
	Side() Side
}

type Clusters []Cluster

func (s *Side) UnmarshalYAML(node *yaml.Node) error {
	switch node.Value {
	case "server":
		*s = Server
	case "client":
		*s = Client
	case "client_and_server":
		*s = ClientAndServer
	default:
		return fmt.Errorf("unknown side value: %q", node.Value)
	}

	return nil
}

func (c Clusters) ReportAttrCount() (count int) {
	for _, cluster := range c {
		count += cluster.ReportAttrCount()
	}

	return count
}

func (c Clusters) Servers() (count int) {
	for _, cluster := range c {
		if cluster.Side()&Server == Server {
			count++
		}
	}

	return count
}

func (c Clusters) Clients() (count int) {
	for _, cluster := range c {
		if cluster.Side()&Client == Client {
			count++
		}
	}

	return count
}

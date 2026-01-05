package nara

import (
	"io/ioutil"
	"log"
	"net"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/weaveworks/mesh"
)

func (network *Network) startMeshServer() error {
	name := mesh.PeerNameFromBin([]byte(network.meName()))

	network.local.Me.MeshPort = network.local.Me.HttpPort + 1

	router, err := mesh.NewRouter(mesh.Config{
		Host:               "",
		Port:               network.local.Me.MeshPort,
		PeerDiscovery:  true,
		TrustedSubnets: []*net.IPNet{},
	}, name, network.meName(), mesh.NullOverlay{}, log.New(ioutil.Discard, "", 0))

	if err != nil {
		logrus.Fatalf("Could not create router: %v", err)
		return err
	}

	peer := newPeer(name, log.New(os.Stderr, "> ", log.LstdFlags))
	gossip, err := router.NewGossip("default", peer) // "default" channel
	if err != nil {
		logrus.Fatalf("Could not create gossip: %v", err)
		return err
	}

	peer.register(gossip)

	logrus.Printf("Listening for mesh on port %d", network.local.Me.MeshPort)

	return nil
}

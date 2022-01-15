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
	logger := log.New(os.Stderr, "> ", log.LstdFlags)

	name := mesh.PeerNameFromBin([]byte(network.meName()))

	network.local.Me.MeshPort = network.local.Me.HttpPort + 1

	router, err := mesh.NewRouter(mesh.Config{
		Host:               network.local.Me.Ip,
		Port:               network.local.Me.MeshPort,
		ProtocolMinVersion: mesh.ProtocolMinVersion,
		// Password:           []byte(*password),
		ConnLimit:      64,
		PeerDiscovery:  true,
		TrustedSubnets: []*net.IPNet{},
	}, name, network.meName(), mesh.NullOverlay{}, log.New(ioutil.Discard, "", 0))

	if err != nil {
		logger.Fatalf("Could not create router: %v", err)
		return err
	}

	peer := newPeer(name, logger)
	gossip, err := router.NewGossip("default", peer) // "default" channel
	if err != nil {
		logger.Fatalf("Could not create gossip: %v", err)
		return err
	}

	peer.register(gossip)

	logrus.Printf("Listening for mesh on %s:%d", network.local.Me.Ip, network.local.Me.MeshPort)

	return nil
}

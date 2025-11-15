package dht

import "net"

type QueryHandler struct {
	krpc    *KRPC
	table   *RoutingTable
	storage *Storage
	token   *TokenManager
}

func NewQueryHandler(
	krpc *KRPC,
	table *RoutingTable,
	storage *Storage,
	token *TokenManager,
) *QueryHandler {
	return &QueryHandler{
		krpc:    krpc,
		table:   table,
		storage: storage,
		token:   token,
	}
}

func (qh *QueryHandler) HandleQuery(msg *Message) {
	senderID, ok := msg.GetNodeID()
	if !ok {
		qh.sendError(msg.T, ErrorProtocol, "invalid node ID", msg.Addr)
		return
	}

	contact := NewContact(&Node{
		ID:   senderID,
		IP:   msg.Addr.IP,
		Port: msg.Addr.Port,
	})
	qh.table.Insert(contact)

	switch msg.Q {
	case PingMethod:
		qh.handlePing(msg)
	case FindNodeMethod:
		qh.handleFindNode(msg)
	case GetPeersMethod:
		qh.handleGetPeers(msg)
	case AnnouncePeerMethod:
		qh.handleAnnouncePeer(msg)
	default:
		qh.sendError(msg.T, ErrorMethodUnknown, "unknown method", msg.Addr)
	}
}

func (qh *QueryHandler) handlePing(msg *Message) {
	response := PingResponse(msg.T, qh.table.ID())
	qh.krpc.SendResponse(response, msg.Addr)
}

func (qh *QueryHandler) handleFindNode(msg *Message) {
	target, ok := msg.GetTarget()
	if !ok {
		qh.sendError(msg.T, ErrorProtocol, "invalid target", msg.Addr)
		return
	}

	contacts := qh.table.FindClosestK(target, K)

	nodes := qh.encodeNodes(contacts)

	response := FindNodeResponse(msg.T, qh.table.ID(), nodes)
	qh.krpc.SendResponse(response, msg.Addr)
}

func (qh *QueryHandler) handleGetPeers(msg *Message) {
	infoHash, ok := msg.GetInfoHash()
	if !ok {
		qh.sendError(msg.T, ErrorProtocol, "invalid info_hash", msg.Addr)
		return
	}

	token := qh.token.Generate(msg.Addr.IP)
	peers := qh.storage.GetPeers(infoHash)

	if len(peers) > 0 {
		values := make([]string, len(peers))
		for i, peer := range peers {
			values[i] = string(peer[:])
		}
		response := GetPeersResponse(msg.T, qh.table.ID(), token, values)
		qh.krpc.SendResponse(response, msg.Addr)
	} else {
		contacts := qh.table.FindClosestK(infoHash, K)
		nodes := qh.encodeNodes(contacts)
		response := GetPeersResponseNodes(msg.T, qh.table.ID(), token, nodes)
		qh.krpc.SendResponse(response, msg.Addr)
	}
}

func (qh *QueryHandler) handleAnnouncePeer(msg *Message) {
	infoHash, ok := msg.GetInfoHash()
	if !ok {
		qh.sendError(msg.T, ErrorProtocol, "invalid info_hash", msg.Addr)
		return
	}

	port, ok := msg.GetPort()
	if !ok {
		qh.sendError(msg.T, ErrorProtocol, "invalid port", msg.Addr)
		return
	}

	token, ok := msg.GetToken()
	if !ok {
		qh.sendError(msg.T, ErrorProtocol, "missing token", msg.Addr)
		return
	}

	if !qh.token.Validate(msg.Addr.IP, token) {
		qh.sendError(msg.T, ErrorProtocol, "invalid token", msg.Addr)
		return
	}

	peerInfo := EncodePeerInfo(msg.Addr.IP, uint16(port))
	qh.storage.StorePeer(infoHash, peerInfo)

	response := AnnouncePeerResponse(msg.T, qh.table.ID())
	qh.krpc.SendResponse(response, msg.Addr)
}

func (qh *QueryHandler) encodeNodes(contacts []*Contact) []byte {
	if len(contacts) == 0 {
		return []byte{}
	}

	// 26 bytes per node (20 byte ID + 4 byte IPv4 + 2 byte port)
	nodes := make([]byte, 0, len(contacts)*26)

	for _, contact := range contacts {
		if info := contact.node.CompactNodeInfo(); info != nil {
			nodes = append(nodes, info...)
		}
	}

	return nodes
}

func (qh *QueryHandler) sendError(
	transactionID string,
	code int,
	message string,
	addr *net.UDPAddr,
) {
	qh.krpc.SendError(transactionID, code, message, addr)
}

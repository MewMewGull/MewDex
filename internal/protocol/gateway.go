package protocol

// GatewayTunnelRequest opens one model TLS byte stream on the configured node.
// Its one-use ID binds the data connection to the authenticated control peer.
type GatewayTunnelRequest struct {
	ID     string `json:"id"`
	Target string `json:"target"`
}

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type xmlNode struct {
	Name     xml.Name
	Attr     []xml.Attr
	Text     string
	Children []*xmlNode
}

func parseXML(raw []byte) (*xmlNode, error) {
	d := xml.NewDecoder(bytes.NewReader(raw))
	root := &xmlNode{}
	stack := []*xmlNode{root}
	for {
		t, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		switch v := t.(type) {
		case xml.StartElement:
			n := &xmlNode{Name: v.Name, Attr: v.Attr}
			p := stack[len(stack)-1]
			p.Children = append(p.Children, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			stack[len(stack)-1].Text += string(v)
		}
	}
	return root, nil
}
func (n *xmlNode) find(name string) []*xmlNode {
	out := []*xmlNode{}
	if n.Name.Local == name {
		out = append(out, n)
	}
	for _, c := range n.Children {
		out = append(out, c.find(name)...)
	}
	return out
}
func (n *xmlNode) first(name string) string {
	items := n.find(name)
	if len(items) > 0 {
		return strings.TrimSpace(items[0].Text)
	}
	return ""
}
func (n *xmlNode) attr(name string) string {
	for _, a := range n.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
func escapeXML(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func validateCameraHost(host string) error {
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return errors.New("กรุณาใช้ IP กล้องในเครือข่าย เช่น 192.168.1.50")
	}
	if !ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() {
		return errors.New("ใช้ได้เฉพาะ IP ภายในเครือข่ายบ้าน")
	}
	return nil
}
func sameCameraURL(raw, host string) bool {
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() == host && u.User == nil
}

type ONVIF struct{ Client *http.Client }

// Only expose operation/status categories; camera responses can contain secrets.
type onvifResponseError struct {
	Operation string
	Status    int
	Auth      bool
	FaultCode string
}

func (e *onvifResponseError) Error() string {
	return fmt.Sprintf("ONVIF %s returned HTTP %d", e.Operation, e.Status)
}

func onvifSetupMessage(err error) string {
	var response *onvifResponseError
	if errors.As(err, &response) {
		if response.Auth {
			message := "กล้องปฏิเสธการยืนยันบัญชี ตรวจ Username และรหัสผ่าน Camera Account ในแอป Tapo รวมถึงเวลาของกล้อง (HTTP " + fmt.Sprint(response.Status) + ")"
			if response.FaultCode != "" {
				message += " · " + response.FaultCode
			}
			return message
		}
		return "ติดต่อกล้องได้ แต่คำขอ " + response.Operation + " ถูกปฏิเสธ (HTTP " + fmt.Sprint(response.Status) + ")"
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		return "กล้องไม่ตอบภายในเวลาที่กำหนด ตรวจว่าเปิดกล้องและเครื่องกลางติดต่อ IP กล้องได้"
	}
	var operation *net.OpError
	if errors.As(err, &operation) {
		return "ติดต่อพอร์ต ONVIF ของกล้องไม่ได้ ตรวจว่า Camera Account ยังเป็น On และกล้องยังอยู่ในเครือข่าย"
	}
	return "ยังเชื่อม ONVIF ไม่ได้ ตรวจ IP, Camera Account และเวลาของกล้อง"
}

func (o *ONVIF) call(ctx context.Context, endpoint, username, password, action, body string) (*xmlNode, error) {
	// Match the printable 16-byte UsernameToken nonce used by the reference
	// go2rtc ONVIF client for firmware interoperability.
	nonce := []byte(randomToken(12))
	created := time.Now().UTC().Format(time.RFC3339Nano)
	sum := sha1.Sum(append(append(append([]byte{}, nonce...), []byte(created)...), []byte(password)...))
	security := fmt.Sprintf(`<wsse:Security xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"><wsse:UsernameToken><wsse:Username>%s</wsse:Username><wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</wsse:Password><wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</wsse:Nonce><wsu:Created>%s</wsu:Created></wsse:UsernameToken></wsse:Security>`, escapeXML(username), base64.StdEncoding.EncodeToString(sum[:]), base64.StdEncoding.EncodeToString(nonce), created)
	envelope := `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Header>` + security + `</s:Header><s:Body>` + body + `</s:Body></s:Envelope>`
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(envelope))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/soap+xml;charset=utf-8")
	// ONVIF authenticates with the WS-Security digest above. Do not also send
	// HTTP Basic credentials: that selects a separate HTTP authentication path.
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	raw, e := limitRead(res.Body, 2<<20)
	if e != nil {
		return nil, e
	}
	node, e := parseXML(raw)
	if res.StatusCode != 200 || (e == nil && len(node.find("Fault")) > 0) {
		operation := action[strings.LastIndex(action, "/")+1:]
		fault := strings.ToLower(string(raw))
		faultCode := ""
		for _, code := range []string{"NotAuthorized", "FailedAuthentication", "InvalidSecurity", "MessageExpired", "MustUnderstand"} {
			if strings.Contains(fault, strings.ToLower(code)) {
				faultCode = code
				break
			}
		}
		auth := res.StatusCode == 401 || res.StatusCode == 403 || strings.Contains(fault, "notauthorized") || strings.Contains(fault, "failedauthentication") || strings.Contains(fault, "invalidsecurity") || strings.Contains(fault, "messageexpired")
		return nil, &onvifResponseError{Operation: operation, Status: res.StatusCode, Auth: auth, FaultCode: faultCode}
	}
	if e != nil {
		return nil, e
	}
	return node, nil
}
func (o *ONVIF) inspect(ctx context.Context, host, username, password string) (Camera, error) {
	c := Camera{Vendor: "tapo", Host: host, Name: "Tapo " + host, Group: "ยังไม่จัดกลุ่ม", Status: "pending", Verification: "untested", LastSeen: time.Now(), Source: Source{Username: username, Password: password, DeviceURL: "http://" + net.JoinHostPort(host, "2020") + "/onvif/device_service"}}
	dev, e := o.call(ctx, c.Source.DeviceURL, username, password, "http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation", "<tds:GetDeviceInformation/>")
	if e != nil {
		return c, e
	}
	c.Model = dev.first("Model")
	c.Ref = dev.first("SerialNumber")
	if c.Ref == "" {
		return c, errors.New("ONVIF camera did not return a stable serial number")
	}
	c.ID = cameraID("tapo", c.Ref)
	caps, e := o.call(ctx, c.Source.DeviceURL, username, password, "http://www.onvif.org/ver10/device/wsdl/GetCapabilities", "<tds:GetCapabilities><tds:Category>All</tds:Category></tds:GetCapabilities>")
	if e != nil {
		return c, e
	}
	for _, n := range caps.find("Media") {
		c.Source.MediaURL = n.first("XAddr")
	}
	for _, n := range caps.find("PTZ") {
		c.Source.PTZURL = n.first("XAddr")
	}
	if !sameCameraURL(c.Source.MediaURL, host) {
		return c, errors.New("invalid ONVIF media service")
	}
	if c.Source.PTZURL != "" && !sameCameraURL(c.Source.PTZURL, host) {
		return c, errors.New("invalid ONVIF PTZ service")
	}
	profiles, e := o.call(ctx, c.Source.MediaURL, username, password, "http://www.onvif.org/ver10/media/wsdl/GetProfiles", "<trt:GetProfiles/>")
	if e != nil {
		return c, e
	}
	canonicalHigh := false
	for i, p := range profiles.find("Profiles") {
		token := p.attr("token")
		if token == "" {
			continue
		}
		n, e := o.call(ctx, c.Source.MediaURL, username, password, "http://www.onvif.org/ver10/media/wsdl/GetStreamUri", "<trt:GetStreamUri><trt:StreamSetup><tt:Stream>RTP-Unicast</tt:Stream><tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport></trt:StreamSetup><trt:ProfileToken>"+escapeXML(token)+"</trt:ProfileToken></trt:GetStreamUri>")
		if e != nil {
			continue
		}
		u, e := url.Parse(n.first("Uri"))
		if e != nil || u.Scheme != "rtsp" || u.Hostname() != host {
			continue
		}
		u.User = url.UserPassword(username, password)
		// Tapo can advertise additional profiles such as stream8. Those are not
		// the standard viewing substream; prefer stream1/stream2 explicitly.
		if c.Source.High == "" || strings.TrimRight(u.Path, "/") == "/stream1" {
			c.Source.High = u.String()
			c.Source.Profile = token
			c.PTZ = c.Source.PTZURL != "" && len(p.find("PTZConfiguration")) > 0
			c.Audio = len(p.find("AudioEncoderConfiguration")) > 0
		}
		if strings.TrimRight(u.Path, "/") == "/stream1" {
			canonicalHigh = true
		}
		if strings.TrimRight(u.Path, "/") == "/stream2" {
			c.Source.Low = u.String()
		}
		if (canonicalHigh && c.Source.Low != "") || i >= 7 {
			break
		}
	}
	if c.Source.High == "" {
		return c, errors.New("no usable RTSP profile")
	}
	if c.Source.Low == "" {
		c.Source.Low = c.Source.High
	}
	c.Source.PTZProtocol = "onvif"
	c.Status = "pending"
	c.Message = "พบกล้องแล้ว ยังไม่ตรวจภาพและเสียง"
	return c, nil
}
func (o *ONVIF) move(ctx context.Context, c Camera, direction string) error {
	if !sameCameraURL(c.Source.PTZURL, c.Host) {
		return errors.New("PTZ endpoint is not valid")
	}
	if direction == "stop" {
		_, e := o.call(ctx, c.Source.PTZURL, c.Source.Username, c.Source.Password, "http://www.onvif.org/ver20/ptz/wsdl/Stop", "<tptz:Stop><tptz:ProfileToken>"+escapeXML(c.Source.Profile)+"</tptz:ProfileToken><tptz:PanTilt>true</tptz:PanTilt><tptz:Zoom>false</tptz:Zoom></tptz:Stop>")
		return e
	}
	x, y := 0.0, 0.0
	switch direction {
	case "left":
		x = -0.35
	case "right":
		x = 0.35
	case "up":
		y = 0.35
	case "down":
		y = -0.35
	default:
		return errors.New("invalid direction")
	}
	// The C520WS advertises PT1S as its minimum. PT0.6S is rejected by
	// real firmware. The hub still sends Stop after release/lease expiry;
	// the camera's own one-second timeout bounds a lost Stop request.
	body := fmt.Sprintf(`<tptz:ContinuousMove><tptz:ProfileToken>%s</tptz:ProfileToken><tptz:Velocity><tt:PanTilt x="%g" y="%g"/></tptz:Velocity><tptz:Timeout>PT1S</tptz:Timeout></tptz:ContinuousMove>`, escapeXML(c.Source.Profile), x, y)
	_, e := o.call(ctx, c.Source.PTZURL, c.Source.Username, c.Source.Password, "http://www.onvif.org/ver20/ptz/wsdl/ContinuousMove", body)
	return e
}
func discoverONVIF(ctx context.Context) ([]string, error) {
	conn, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if e != nil {
		return nil, e
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2500 * time.Millisecond))
	uid := make([]byte, 16)
	_, _ = rand.Read(uid)
	uid[6] = (uid[6] & 15) | 64
	uid[8] = (uid[8] & 63) | 128
	messageID := fmt.Sprintf("%x-%x-%x-%x-%x", uid[:4], uid[4:6], uid[6:8], uid[8:10], uid[10:])
	msg := `<?xml version="1.0"?><e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl"><e:Header><w:MessageID>uuid:` + messageID + `</w:MessageID><w:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To><w:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action></e:Header><e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body></e:Envelope>`
	_, e = conn.WriteToUDP([]byte(msg), &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 3702})
	if e != nil {
		return nil, e
	}
	hosts := map[string]bool{}
	buf := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		n, _, e := conn.ReadFromUDP(buf)
		if e != nil {
			break
		}
		node, e := parseXML(buf[:n])
		if e != nil {
			continue
		}
		for _, addr := range node.find("XAddrs") {
			for _, s := range strings.Fields(addr.Text) {
				u, e := url.Parse(s)
				if e == nil && validateCameraHost(u.Hostname()) == nil {
					hosts[u.Hostname()] = true
				}
			}
		}
	}
	out := []string{}
	for h := range hosts {
		out = append(out, h)
	}
	return out, nil
}

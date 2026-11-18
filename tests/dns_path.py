"""Probe strict sing-box DNS with a local DoH canary and actual Shadowsocks transport."""
import base64
import http.server
import json
import socket
import ssl
import struct
import threading
import time
from pathlib import Path

def exact(sock, size):
    data = b""
    while len(data) < size:
        part = sock.recv(size-len(data))
        if not part:
            raise ConnectionError("DNS connection closed")
        data += part
    return data

def query(port, name):
    # SOCKS5 -> client route hijack-dns -> typed DoH -> selected proxy -> canary.
    with socket.create_connection(("127.0.0.1", port), timeout=4) as sock:
        sock.sendall(bytes.fromhex("050100"))
        assert exact(sock,2) == bytes.fromhex("0500")
        sock.sendall(bytes.fromhex("05010001c00002350035"))
        status = exact(sock,4)
        assert status[1] == 0
        exact(sock, {1:4,4:16}[status[3]]+2)
        labels = b"".join(bytes([len(x)]) + x.encode() for x in name.split(".")) + bytes(1)
        msg = bytes.fromhex("115501000001000000000000") + labels + bytes.fromhex("00010001")
        sock.sendall(struct.pack("!H",len(msg))+msg)
        return exact(sock,struct.unpack("!H",exact(sock,2))[0])

def check(a, lab, ws, cli, cert, key, running, port):
    seen=[]
    class Canary(http.server.BaseHTTPRequestHandler):
        def do_POST(self):
            msg=self.rfile.read(int(self.headers["content-length"]))
            self.reply(msg)
        def do_GET(self):
            from urllib.parse import parse_qs, urlsplit
            raw=parse_qs(urlsplit(self.path).query)["dns"][0]
            self.reply(base64.urlsafe_b64decode(raw+"==="))
        def reply(self,msg):
            seen.append(msg)
            end=12
            while msg[end]:
                end+=1+msg[end]
            question=msg[12:end+5]
            answer=msg[:2]+bytes.fromhex("81800001000100000000")+question+bytes.fromhex("c00c00010001000000010004c000025b")
            self.send_response(200)
            self.send_header("content-type","application/dns-message")
            self.send_header("content-length",str(len(answer)))
            self.end_headers()
            self.wfile.write(answer)
        def log_message(self,*_):
            pass
    server=http.server.ThreadingHTTPServer(("127.0.0.1",0),Canary)
    ctx=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(cert,key)
    server.socket=ctx.wrap_socket(server.socket,server_side=True)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    try:
        cfg=json.loads(cli("subscription","show","--format","sing-box","--filter","direct-ss"))
        local=port()
        cfg["inbounds"][0]["listen_port"]=local
        for dns in cfg["dns"]["servers"]:
            dns.update(server="127.0.0.1",server_port=server.server_port,
                       tls={"enabled":True,"server_name":"cdn.example.test","certificate":Path(cert).read_text()})
        file=lab/"dns-client.json"
        file.write_text(json.dumps(cfg))
        with running([a.core,"run","-c",file],lab):
            with running([a.core,"run","-c",ws/"current/server.json"],lab):
                time.sleep(.6)
                assert query(local,"permitted.example").endswith(bytes.fromhex("c000025b"))
                assert any(b"permitted" in msg for msg in seen)
            count=len(seen)
            try:
                response=query(local,"blocked.example")
                assert response[3] & 15 != 0, "DNS succeeded after proxy loss"
            except (OSError,ConnectionError):
                pass
            assert count==len(seen), "DoH canary saw direct fallback"
        print("PASS strict DNS through proxy and fail-closed after proxy loss",flush=True)
    finally:
        server.shutdown()
        server.server_close()

import net from 'node:net';
import dgram from 'node:dgram';
import {readFile} from 'node:fs/promises';
import {readFileSync} from 'node:fs';
import {dirname, join} from 'node:path';
import {timingSafeEqual} from 'node:crypto';
import {pathToFileURL} from 'node:url';

// Authenticated, loopback-only transport for CS2 camera datagrams. It neither
// decrypts camera traffic nor accepts arbitrary network destinations.
const privateIPv4 = h => {
  if (net.isIP(h) !== 4) return false;
  const [a,b] = h.split('.').map(Number);
  return a === 10 || a === 192 && b === 168 || a === 172 && b >= 16 && b <= 31;
};
export function createRelay({token, hosts, getHosts = () => hosts, port = 17993, maxSessions = 32, socketFactory = () => dgram.createSocket('udp4')}) {
  if (!/^[a-f0-9]{64}$/.test(token) || !Array.isArray(hosts) || hosts.some(h => !privateIPv4(h)) || !Number.isInteger(port) || (port !== 0 && (port < 17993 || port > 17999)))
    throw new Error('Invalid private relay settings');
  const clients = new Set();
  const server = net.createServer(socket => {
    if (clients.size >= maxSessions) {socket.destroy(); return;}
    clients.add(socket);
    // CS2 uses many small ACK/control packets; avoid Nagle delays in the tunnel.
    socket.setNoDelay(true);
    socket.setKeepAlive(true, 30000);
    let buffer = Buffer.alloc(0), udp, target, authenticated = false;
    const learnedPorts = new Set([32108]);
    socket.setTimeout(60000, () => socket.destroy());
    const authTimer = setTimeout(() => {if (!authenticated) socket.destroy();}, 3000);
    socket.on('error', () => {});
    socket.on('close', () => {clearTimeout(authTimer); clients.delete(socket); if (udp) {try {udp.close();} catch {}}});
    socket.on('data', chunk => {
      buffer = Buffer.concat([buffer, chunk]);
      if (!authenticated) {
        const end = buffer.indexOf(10);
        if (end < 0) {if (buffer.length > 1024) socket.destroy(); return;}
        if (end > 1024) {socket.destroy(); return;}
        let hello;
        try {hello = JSON.parse(buffer.subarray(0, end).toString());} catch {socket.destroy(); return;}
        if (!hello || typeof hello !== 'object') {socket.destroy(); return;}
        const supplied = Buffer.from(typeof hello.token === 'string' ? hello.token : '');
        let allowed = [];
        try {const current = getHosts(); if (Array.isArray(current) && current.every(privateIPv4)) allowed = current;} catch {}
        if (supplied.length !== 64 || !timingSafeEqual(supplied, Buffer.from(token)) || !allowed.includes(hello.host)) {
          socket.destroy(); return;
        }
        authenticated = true; clearTimeout(authTimer); target = hello.host;
        buffer = buffer.subarray(end + 1);
        udp = socketFactory();
        udp.on('error', () => socket.destroy());
        udp.on('listening', () => {try {udp.setRecvBufferSize(1024 * 1024);} catch {}});
        udp.on('message', (packet, from) => {
          if (from.address !== target || from.port < 1024 || packet.length > 2048 || packet.length < 4 || packet[0] !== 0xf1) return;
          learnedPorts.add(from.port);
          if (socket.writableLength > 4 * 1024 * 1024) {socket.destroy(); return;}
          const frame = Buffer.allocUnsafe(packet.length + 4);
          frame.writeUInt16BE(from.port, 0); frame.writeUInt16BE(packet.length, 2); packet.copy(frame, 4);
          socket.write(frame);
        });
        // Datagram sends auto-bind the socket. No camera account is present here.
        socket.write(Buffer.from([1]));
      }
      while (buffer.length >= 4) {
        const destinationPort = buffer.readUInt16BE(0), size = buffer.readUInt16BE(2);
        if (size < 4 || size > 2048 || !learnedPorts.has(destinationPort)) {socket.destroy(); return;}
        if (buffer.length < size + 4) return;
        const packet = buffer.subarray(4, size + 4);
        if (packet[0] !== 0xf1) {socket.destroy(); return;}
        udp.send(packet, destinationPort, target, error => {if (error) socket.destroy();});
        buffer = buffer.subarray(size + 4);
      }
    });
  });
  return {server, listen: () => new Promise((resolve, reject) => {
    server.once('error', reject); server.listen(port, '127.0.0.1', resolve);
  }), close: () => {for (const c of clients) c.destroy(); server.close();}};
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const settings = JSON.parse(await readFile(process.argv[2], 'utf8'));
    const hostsPath = join(dirname(process.argv[2]), 'relay-hosts.json');
    const getHosts = () => JSON.parse(readFileSync(hostsPath, 'utf8'));
    const relay = createRelay({token: settings.api.password, hosts: getHosts(), getHosts, port: settings.relay.port});
    await relay.listen();
    for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => relay.close());
  } catch {console.error('Camera UDP relay could not start. Check its private settings and port.'); process.exitCode = 1;}
}

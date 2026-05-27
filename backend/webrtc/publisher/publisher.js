/**
 * WebRTC publisher client for vehicle live video.
 * Works over the internet when API is served over HTTPS and TURN is configured.
 */
(function () {
  const $ = (id) => document.getElementById(id);
  const statusEl = $('status');
  const logEl = $('log');

  const params = new URLSearchParams(location.search);

  function defaultApiBase() {
    if (params.get('apiBase')) return params.get('apiBase');
    const stored = localStorage.getItem('webrtc_api_base');
    if (stored) return stored;
    // When publisher is served from the API host (production / internet)
    if (location.pathname.includes('/api/')) {
      return `${location.origin}/api`;
    }
    // Local dev fallback
    return `${location.protocol}//${location.hostname}:8081/api`;
  }

  if (params.get('vehicleId')) $('vehicleId').value = params.get('vehicleId');
  if (params.get('token')) $('token').value = params.get('token');
  $('apiBase').value = defaultApiBase();
  if (params.get('signalingUrl')) {
    $('signalingUrl').value = params.get('signalingUrl');
  } else if (localStorage.getItem('webrtc_signaling_url')) {
    $('signalingUrl').value = localStorage.getItem('webrtc_signaling_url');
  }

  let ws = null;
  let localStream = null;
  const peerConnections = new Map();
  const pendingIceCandidates = new Map();
  let iceServers = [{ urls: 'stun:stun.l.google.com:19302' }];
  let vehicleId = '';
  let token = '';
  const logLines = [];

  function setStatus(text, cls) {
    statusEl.textContent = text;
    statusEl.className = cls;
  }

  function log(msg) {
    const line = `${new Date().toLocaleTimeString()} ${msg}`;
    logLines.push(line);
    logEl.textContent = logLines.slice(-10).join('\n');
    console.log('[publisher]', msg);
  }

  function signalingUrl() {
    const override = $('signalingUrl').value.trim();
    if (override) return override;
    const base = $('apiBase').value.replace(/\/$/, '').replace(/\/api$/, '');
    const wsProto = base.startsWith('https') ? 'wss' : 'ws';
    const host = base.replace(/^https?:\/\//, '');
    return `${wsProto}://${host}/api/webrtc/ws`;
  }

  if (!$('signalingUrl').value.trim()) {
    $('signalingUrl').value = signalingUrl();
  }
  function send(msg) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg));
    }
  }

  async function createPeerForViewer(viewerId) {
    const pc = new RTCPeerConnection({ iceServers });
    peerConnections.set(viewerId, pc);
    pendingIceCandidates.set(viewerId, []);

    localStream.getTracks().forEach((track) => pc.addTrack(track, localStream));

    pc.onicecandidate = (e) => {
      if (e.candidate) {
        send({
          type: 'ice_candidate',
          vehicleId,
          viewerId,
          candidate: e.candidate.toJSON(),
        });
      }
    };

    pc.onconnectionstatechange = () => {
      log(`Viewer ${viewerId}: ${pc.connectionState}`);
    };
    pc.oniceconnectionstatechange = () => {
      log(`Viewer ${viewerId} ICE: ${pc.iceConnectionState}`);
    };

    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    send({
      type: 'offer',
      vehicleId,
      viewerId,
      sdp: offer.sdp,
    });
  }

  async function handleSignal(msg) {
    const type = msg.type;
    switch (type) {
      case 'joined':
        iceServers = msg.iceServers || iceServers;
        setStatus('Live - waiting for viewers', 'live');
        break;
      case 'viewer_joined': {
        const vid = msg.viewerId;
        if (vid) await createPeerForViewer(vid);
        break;
      }
      case 'answer': {
        const vid = msg.viewerId;
        const pc = peerConnections.get(vid);
        if (pc && msg.sdp) {
          await pc.setRemoteDescription({ type: 'answer', sdp: msg.sdp });
          await flushPendingIce(vid, pc);
        }
        break;
      }
      case 'ice_candidate': {
        const vid = msg.viewerId;
        const pc = peerConnections.get(vid);
        if (pc && msg.candidate) {
          if (!pc.remoteDescription) {
            queueIce(vid, msg.candidate);
          } else {
            await addIceCandidate(pc, msg.candidate);
          }
        }
        break;
      }
      case 'viewer_disconnected': {
        const vid = msg.viewerId;
        const pc = peerConnections.get(vid);
        if (pc) pc.close();
        peerConnections.delete(vid);
        pendingIceCandidates.delete(vid);
        break;
      }
      case 'error':
        setStatus(msg.message || 'Error', 'failed');
        log(msg.message);
        break;
    }
  }

  function queueIce(viewerId, candidate) {
    if (!pendingIceCandidates.has(viewerId)) {
      pendingIceCandidates.set(viewerId, []);
    }
    pendingIceCandidates.get(viewerId).push(candidate);
  }

  async function addIceCandidate(pc, candidate) {
    try {
      await pc.addIceCandidate(candidate);
    } catch (e) {
      console.warn(e);
      log(`ICE candidate rejected: ${e.message || e}`);
    }
  }

  async function flushPendingIce(viewerId, pc) {
    const queued = pendingIceCandidates.get(viewerId) || [];
    pendingIceCandidates.set(viewerId, []);
    for (const candidate of queued) {
      await addIceCandidate(pc, candidate);
    }
  }

  function cleanup() {
    peerConnections.forEach((pc) => pc.close());
    peerConnections.clear();
    pendingIceCandidates.clear();
    if (localStream) {
      localStream.getTracks().forEach((t) => t.stop());
      localStream = null;
    }
    $('preview').srcObject = null;
    if (ws) {
      ws.close();
      ws = null;
    }
    $('btnStart').disabled = false;
    $('btnStop').disabled = true;
    setStatus('Offline', 'offline');
  }

  async function startStream() {
    vehicleId = $('vehicleId').value.trim();
    token = $('token').value.trim();
    const apiBase = $('apiBase').value.trim();
    localStorage.setItem('webrtc_api_base', apiBase);
    localStorage.setItem('webrtc_signaling_url', $('signalingUrl').value.trim());

    if (!vehicleId || !token) {
      alert('Vehicle ID and token are required');
      return;
    }

    cleanup();
    vehicleId = $('vehicleId').value.trim();
    token = $('token').value.trim();
    setStatus('Connecting...', 'connecting');
    $('btnStart').disabled = true;
    log(`Starting publisher for vehicle ${vehicleId}`);

    try {
      log('Requesting camera permission');
      localStream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: 'environment' },
        audio: false,
      });
      log('Camera stream acquired');
      $('preview').srcObject = localStream;
    } catch (e) {
      setStatus('Camera denied', 'failed');
      log(`Camera failed: ${e.name || 'Error'} ${e.message || ''}`.trim());
      $('btnStart').disabled = false;
      return;
    }

    const wsUrl = signalingUrl();
    log(`Opening WebSocket: ${wsUrl}`);
    ws = new WebSocket(wsUrl);
    ws.onopen = () => {
      log('WebSocket open; joining as publisher');
      send({ type: 'publisher_join', vehicleId, token });
    };
    ws.onmessage = async (ev) => {
      const msg = JSON.parse(ev.data);
      log(`Signal received: ${msg.type || 'unknown'}`);
      await handleSignal(msg);
    };
    ws.onerror = (e) => {
      setStatus('WebSocket failed', 'failed');
      log(`WebSocket failed: ${e.message || 'browser blocked or connection failed'}`);
      cleanup();
    };
    ws.onclose = (e) => {
      log(`WebSocket closed: ${e.code || ''} ${e.reason || ''}`.trim());
      if (statusEl.classList.contains('live')) setStatus('Disconnected', 'offline');
    };

    $('btnStop').disabled = false;
  }

  function stopStream() {
    send({ type: 'stream_stopped', vehicleId });
    cleanup();
  }

  $('btnStart').onclick = startStream;
  $('btnStop').onclick = stopStream;
})();

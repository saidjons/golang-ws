let socket=new WebSocket("ws://localhost:8080/ws");

 


 
socket.onopen = () => {
  console.log("✅ Connected");
  socket.send(JSON.stringify("Hello from browser!")); // must be JSON string
};

socket.onmessage = (event) => {
  console.log("📨 Message from server:", event.data);
};

socket.onerror = (err) => {
  console.error("❌ WebSocket error:", err);
};

socket.onclose = () => {
  console.log("🔒 Connection closed");
};

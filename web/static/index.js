// Create a WebSocket connection
const socket = new WebSocket("ws://localhost:4000/ws");

// Event handler for when the connection is opened
socket.onopen = function(event) {
    console.log("WebSocket connection opened.");
    // Send a message to the server after the connection is established
    // socket.send(JSON.parse({msg: "hello"}))
};

// Event handler for when the connection is closed
socket.onclose = function(event) {
    if (event.wasClean) {
        console.log(`WebSocket connection closed cleanly, code=${event.code}, reason=${event.reason}`);
    } else {
        console.error("WebSocket connection abruptly closed.");
    }
};

// Event handler for WebSocket errors
socket.onerror = function(error) {
    console.error("WebSocket error:", error);
};

// Event handler for when a message is received from the server
socket.onmessage = function(event) {
    console.log("Message received from server:", event.data);
    // Handle the received message here
};
let socket;

let chart = LightweightCharts.createChart("chart-container", {
	width: 1200,
    height: 600,
	crosshair: {
		mode: LightweightCharts.CrosshairMode.Normal,
	},
    timeScale: {
        timeVisible: true,
        secondsVisible: true
    }
});

let candleSeries = chart.addCandlestickSeries();

// Get a reference to the search button element
const searchButton = document.getElementById("search-btn");

// Add a click event listener to the search button
searchButton.addEventListener("click", function(event) {
    event.preventDefault();

    // Get the selected trading pair and timeframe from the form
    const symbol = document.getElementById("symbol").value;
    const timeframe = document.getElementById("timeframe").value;

    createWebSocketConnection(symbol, timeframe)

});

chart.timeScale().fitContent();


// Function to create a WebSocket connection
function createWebSocketConnection(symbol, timeframe) {
    // Close the existing WebSocket connection, if it exists
    if (socket && socket.readyState === WebSocket.OPEN) {
        console.log("WebSocket stream already open. Closing it...")
        socket.send("CLOSE")
        socket.close();
    }
    // Create a WebSocket connection
    socket = new WebSocket(`ws://localhost:4000/ws?symbol=${symbol}&timeframe=${timeframe}`);

    // Event handler for when the connection is opened
    socket.onopen = function(event) {
        console.log("WebSocket connection opened.");
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
        // console.log("Message received from server:", event.data);
        // Handle the received message here

        // Parse JSON String to JavaScript object
        // not sure why have to do it 2x to work
        const jsonObject = JSON.parse(JSON.parse(event.data));

        // Kline data is in 'data': {k: ...}' object
        const candleStick = jsonObject.data.k
        console.log(candleStick)

        candleSeries.update({
            time: candleStick.t / 1000,
            open: parseFloat(candleStick.o),
            high: parseFloat(candleStick.h),
            low: parseFloat(candleStick.l),
            close: parseFloat(candleStick.c)
        })
    };
};

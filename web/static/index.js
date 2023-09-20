var chart = LightweightCharts.createChart("chart-container", {
	width: 600,
    height: 300,
	layout: {
		background: {
            type: 'solid',
            color: '#000000',
        },
		textColor: 'rgba(255, 255, 255, 0.9)',
	},
	grid: {
		vertLines: {
			color: 'rgba(197, 203, 206, 0.5)',
		},
		horzLines: {
			color: 'rgba(197, 203, 206, 0.5)',
		},
	},
	crosshair: {
		mode: LightweightCharts.CrosshairMode.Normal,
	},
	rightPriceScale: {
		borderColor: 'rgba(197, 203, 206, 0.8)',
	},
	timeScale: {
		borderColor: 'rgba(197, 203, 206, 0.8)',
	},
});

var candleSeries = chart.addCandlestickSeries({
  upColor: 'rgba(255, 144, 0, 1)',
  downColor: '#000',
  borderDownColor: 'rgba(255, 144, 0, 1)',
  borderUpColor: 'rgba(255, 144, 0, 1)',
  wickDownColor: 'rgba(255, 144, 0, 1)',
  wickUpColor: 'rgba(255, 144, 0, 1)',
});


// Get a reference to the search button element
const searchButton = document.getElementById("search-btn");

// Add a click event listener to the search button
searchButton.addEventListener("click", function(event) {
    event.preventDefault(); // Prevent the default form submission behavior
    
    // Get the selected trading pair and timeframe from the form
    const symbol = document.getElementById("symbol").value;
    const timeframe = document.getElementById("timeframe").value;

    console.log(symbol, timeframe)
    // Create a WebSocket connection
    const socket = new WebSocket("ws://localhost:4000/ws", "http");

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
        const msg = JSON.parse(event.data)
        console.log(msg)

        // Kline data is in 'k' object
        // const candleStick = msg.k

        // // candleSeries.setData();
        // candleSeries.update({
        //     time: candleSeries.t / 1000,
        //     open: candleSeries.o,
        //     high: candleSeries.h,
        //     low: candleSeries.l,
        //     close: candleSeries.c
        // })
    };
});
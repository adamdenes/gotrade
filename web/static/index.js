let socket;
let controller;
let buyMarkers = [];
let sellMarkers = [];

let chart = LightweightCharts.createChart("chart-container", {
  // width: 1200,
  // height: 600,
  crosshair: {
    mode: LightweightCharts.CrosshairMode.Normal,
  },
  timeScale: {
    timeVisible: true,
    secondsVisible: false,
  },
});

let candleSeries = chart.addCandlestickSeries();

chart.timeScale().fitContent();

document.addEventListener("DOMContentLoaded", function () {
  let searchForm;
  let backtestForm;
  let startBotForm;

  if (window.location.pathname === "/") {
    startBotForm = document.getElementById("startbot-form");
    startBotForm.addEventListener("submit", startBot);
  } else if (window.location.pathname === "/klines/live") {
    searchForm = document.getElementById("search-form");
    searchForm.addEventListener("submit", getLive);
  } else if (window.location.pathname === "/backtest") {
    backtestForm = document.getElementById("backtest-form");
    backtestForm.addEventListener("submit", getBacktest);
  } else {
    if (startBotForm) startBotForm.removeEventListener("submit", startBot);
    if (searchForm) searchForm.removeEventListener("submit", getLive);
    if (backtestForm) backtestForm.removeEventListener("submit", getBacktest);
  }
});

window.addEventListener("beforeunload", function (event) {
  controller.abort();
});

// Function to create a WebSocket connection
function createWebSocketConnection(symbol, interval, strategy, localFlag) {
  // Close the existing WebSocket connection, if it exists
  if (socket && socket.readyState === WebSocket.OPEN) {
    console.log("WebSocket stream already open. Closing it...");
    closeWebSocketConnection(socket);
  }

  connStr = `ws://localhost:4000/ws?symbol=${symbol}&interval=${interval}`;
  connStr += strategy != "" ? `&strategy=${strategy}` : "";
  connStr += localFlag ? "&local=true" : "&local=false";
  socket = new WebSocket(connStr);

  // Event handler for when the connection is opened
  socket.onopen = function (event) {
    console.log("WebSocket connection opened.");
  };

  // Event handler for when the connection is closed
  socket.onclose = function (event) {
    if (event.wasClean) {
      console.log(
        `WebSocket connection closed cleanly, code=${event.code}, reason=${event.reason}`,
      );
    } else {
      console.error("WebSocket connection abruptly closed.");
    }
  };

  // Event handler for WebSocket errors
  socket.onerror = function (error) {
    console.error("WebSocket error:", error);
  };

  // Event handler for when a message is received from the server
  socket.onmessage = function (event) {
    // Handle the received message here

    // Parse JSON String to JavaScript object
    // have to parse 2x due to JSON string
    const jsonString = JSON.parse(event.data);

    if (jsonString.side) {
      // This is an order message, visualize it
      if (jsonString.side === "BUY") {
        // Create a marker for buy order
        buyMarkers.push({
          time: jsonString.timestamp / 1000,
          position: "belowBar",
          color: "green",
          shape: "arrowUp",
        });
      } else if (jsonString.side === "SELL") {
        // Create a marker for sell order
        sellMarkers.push({
          time: jsonString.timestamp / 1000,
          position: "aboveBar",
          color: "red",
          shape: "arrowDown",
        });
      }

      // Update the chart with the new markers
      candleSeries.setMarkers(buyMarkers.concat(sellMarkers));
    } else {
      const jsonObject = JSON.parse(jsonString);
      // Kline data is in 'data': {k: ...}' object
      const candleStick = jsonObject.data.k;

      candleSeries.update({
        time: candleStick.t / 1000,
        open: candleStick.o,
        high: candleStick.h,
        low: candleStick.l,
        close: candleStick.c,
      });
    }
  };
}

function closeWebSocketConnection(socket) {
  socket.send("CLOSE");
  socket.close();
}

function startBot(event) {
  event.preventDefault();
  const selectedStrategy = document.getElementById("strat-bt").value;
  const symbol = document.getElementById("symbol-home").value;
  const interval = document.getElementById("interval-bt").value;

  fetch("/start-bot", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      symbol: symbol,
      interval: interval,
      strategy: selectedStrategy,
    }),
  })
    .then((response) => response.json())
    .then((data) => {
      console.log(data);
      if (data === "success") {
        // Append a new bot element to the dashboard
        let botElement = document.createElement("div");
        botElement.className = "bot";
        botElement.innerHTML = `
                    <h3>Bot using ${selectedStrategy}</h3>
                    <p>Status: Active</p>
                    <p>Running Since: ${new Date().toLocaleString()}</p>
                    <!-- Add other details as needed -->
                `;

        // Assuming your running bots are in a container with class 'running-bots'
        document.querySelector(".running-bots").appendChild(botElement);

        alert("Bot started successfully using " + selectedStrategy + "!");
      } else {
        alert("Failed to start the bot: " + data.error);
      }
    })
    .catch((error) => {
      console.error("Error:", error);
      alert("Failed to start the bot. Please try again.");
    });
}

function getLive(event) {
  event.preventDefault();
  // Reset the CandleSeries before loading data again
  candleSeries.setData([]);

  // Get the selected trading pair and interval from the form
  const symbol = document.getElementById("symbol-chart").value;
  const interval = document.getElementById("interval-chart").value;

  // If the user is quickly switcing pages mid-fetch 'broken pipe' error occurs on server side
  controller = new AbortController();
  const signal = controller.signal;

  // klines?symbol=BNBBTC&interval=1m&limit=1000
  fetch("/klines/live", {
    signal: signal,
    method: "POST",
    body: JSON.stringify({
      symbol: symbol,
      interval: interval,
    }),
    headers: {
      "Content-Type": "application/json",
    },
  })
    .then((response) => {
      if (!response) {
        throw new Error(`HTTP error! Status: ${response.status}`);
      }
      return response.json();
    })
    .then((data) => {
      const historicalData = data.map((d) => {
        return {
          time: d[0] / 1000,
          open: parseFloat(d[1]),
          high: parseFloat(d[2]),
          low: parseFloat(d[3]),
          close: parseFloat(d[4]),
        };
      });
      candleSeries.setData(historicalData);
      createWebSocketConnection(symbol, interval, "", false);
    })
    .catch((err) => {
      if (err.name === "AbortError") {
        console.log("Fetch request was aborted.");
      } else {
        console.error("Error:", err);
      }
      // Close the ongoing WebSocket connection, if any
      if (socket && socket.readyState === WebSocket.OPEN) {
        // socket is accessible from outer function scope
        closeWebSocketConnection(socket);
      }
    });
}

function getBacktest(event) {
  event.preventDefault();
  const symbol = document.querySelector("#symbol").value;
  const interval = document.querySelector("#interval-bt").value;
  const startTime = document.querySelector("#open_time").value;
  const endTime = document.querySelector("#close_time").value;
  const strategy = document.querySelector("#strat-bt").value;

  candleSeries.setData([]);

  // If the user is quickly switcing pages mid-fetch 'broken pipe' error occurs on server side
  controller = new AbortController();
  const signal = controller.signal;

  fetch("/fetch-data", {
    signal: signal,
    method: "POST",
    body: JSON.stringify({
      symbol: symbol,
      interval: interval,
      open_time: new Date(startTime),
      close_time: new Date(endTime),
      strategy: strategy,
    }),
    headers: {
      "Content-Type": "application/json",
      "Accept-Encoding": "gzip",
    },
  })
    .then((response) => {
      if (response.ok) {
        return response.json();
      }
    })
    .then((data) => {
      const historicalData = data.map((d) => {
        return {
          time: new Date(d.open_time).getTime() / 1000,
          open: d.open,
          high: d.high,
          low: d.low,
          close: d.close,
        };
      });
      candleSeries.setData(historicalData);

      const smaSeries = chart.addLineSeries({
        color: "blue", // Customize the color
        lineWidth: 2, // Customize the line width
      });
      const smaData1 = calculateSMA(historicalData, 12);
      smaSeries.setData(smaData1);

      const smaSeries2 = chart.addLineSeries({
        color: "purple", // Customize the color
        lineWidth: 2, // Customize the line width
      });
      const smaData2 = calculateSMA(historicalData, 24);
      smaSeries2.setData(smaData2);

      createWebSocketConnection(symbol, interval, strategy, true);

      // TODO: figure out a better way / assign timeout based on interval?
      // for 1m interval 800 seems to be fine
      // for the rest even 100ms could be good
      setTimeout(() => {
        for (let index = 0; index < historicalData.length; index++) {
          const element = JSON.stringify([historicalData[index]]);
          socket.send(element);
        }
      }, 800);
    })
    .catch((err) => {
      if (err.name === "AbortError") {
        console.log("Fetch request was aborted.");
      } else {
        console.error("Error:", err);
      }
    });
}

chart.timeScale().subscribeVisibleTimeRangeChange((visibleRange) => {
  if (visibleRange) {
    console.log(visibleRange);
    // Check if the user has scrolled to the start or end
  }
});

// Calculate SMA values
const calculateSMA = (data, period) => {
  const sma = [];
  for (let i = period - 1; i < data.length; i++) {
    const sum = data
      .slice(i - period + 1, i + 1)
      .reduce((acc, item) => acc + item.close, 0);
    sma.push({ time: data[i].time, value: sum / period });
  }
  return sma;
};

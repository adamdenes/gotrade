let socket;
let controller;

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

  if (window.location.pathname === "/backtest") {
    backtestForm = document.getElementById("backtest-form");
    backtestForm.addEventListener("submit", getBacktest);
  } else if (window.location.pathname === "/klines/live") {
    searchForm = document.getElementById("search-form");
    searchForm.addEventListener("submit", getLive);
  } else {
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
    // console.log("Message received from server:", event.data);
    // Handle the received message here

    // Parse JSON String to JavaScript object
    // have to parse 2x due to JSON string
    const jsonString = JSON.parse(event.data);
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
  };
}

function closeWebSocketConnection(socket) {
  socket.send("CLOSE");
  socket.close();
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
  console.log(strategy);

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

/* Dashboard charts.

   Chart.js draws to a canvas, so it can't inherit CSS. Instead of duplicating
   the palette here, every colour and font is read back out of the CSS custom
   properties at startup -- tokens.css stays the single source of truth, and
   retuning the theme there retunes the charts with it. */

const css = getComputedStyle(document.documentElement);
const token = (name) => css.getPropertyValue(name).trim();

const theme = {
	ink: token("--nb-ink"),
	surface: token("--nb-surface"),
	yellow: token("--nb-yellow"),
	pink: token("--nb-pink"),
	blue: token("--nb-blue"),
	green: token("--nb-green"),
	orange: token("--nb-orange"),
	fontBody: token("--nb-font-body"),
	fontMono: token("--nb-font-mono"),
	// Charts are canvas, so the hard shadow can't be drawn on the bars. The
	// surrounding .nb-card carries it instead; inside, the look comes from the
	// same heavy black stroke.
	borderWidth: parseInt(token("--nb-border-width"), 10) || 3,
};

Chart.register(ChartDataLabels);

Chart.defaults.font.family = theme.fontBody;
Chart.defaults.font.weight = 700;
Chart.defaults.color = theme.ink;
Chart.defaults.borderColor = theme.ink;
// Titles and legends live in the HTML card header, so the canvas doesn't
// repeat them.
Chart.defaults.plugins.title.display = false;
Chart.defaults.plugins.legend.display = false;

/* Axes: thin black rules, mono tick labels, no soft greys anywhere. */
function axes(options = {}) {
	const scale = {
		border: { color: theme.ink, width: theme.borderWidth },
		grid: { color: theme.ink, lineWidth: 1, drawTicks: false },
		ticks: {
			color: theme.ink,
			font: { family: theme.fontMono, size: 11, weight: 700 },
			padding: 6,
		},
	};

	return {
		x: { ...scale, grid: { ...scale.grid, display: false } },
		y: { ...scale, beginAtZero: true, ...options.y },
	};
}

/* Value labels sitting above the bars, in the mono token font. */
const barLabels = {
	anchor: "end",
	align: "top",
	offset: 2,
	color: theme.ink,
	font: { family: theme.fontMono, size: 12, weight: 700 },
};

function barDataset(label, data, fill) {
	return {
		label,
		data,
		backgroundColor: fill,
		borderColor: theme.ink,
		borderWidth: theme.borderWidth,
		// Square corners, like everything else.
		borderRadius: 0,
		borderSkipped: false,
	};
}

const baseOptions = {
	responsive: true,
	maintainAspectRatio: false,
	layout: { padding: { top: 18 } },
};

function shortResolverName(url) {
	return url.replace(/^https?:\/\//, "");
}

async function fetchJSON(path) {
	const resp = await fetch(path);
	if (!resp.ok) {
		throw new Error(`${path} returned ${resp.status}`);
	}
	return resp.json();
}

/* A blank canvas tells the viewer nothing, so a failed chart says so in place
   rather than only in the console. */
function renderChartError(canvasId, err) {
	console.error(err);
	const canvas = document.getElementById(canvasId);
	if (!canvas || !canvas.parentElement) {
		return;
	}
	const notice = document.createElement("p");
	notice.className = "nb-code nb-fill-pink";
	notice.textContent = "No data: " + err.message;
	canvas.parentElement.replaceChildren(notice);
}

async function renderSuccessChart() {
	const points = await fetchJSON("/api/stats/success");
	new Chart(document.getElementById("successChart"), {
		type: "bar",
		data: {
			labels: points.map((p) => shortResolverName(p.resolver)),
			datasets: [barDataset("Successful embeds", points.map((p) => p.value), theme.green)],
		},
		options: { ...baseOptions, scales: axes(), plugins: { datalabels: barLabels } },
	});
}

async function renderRequestsChart() {
	const points = await fetchJSON("/api/stats/requests-timeseries");
	new Chart(document.getElementById("requestsChart"), {
		type: "line",
		data: {
			labels: points.map((p) =>
				new Date(p.timestamp * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
			),
			datasets: [{
				label: "Requests (per 10min)",
				data: points.map((p) => p.value),
				borderColor: theme.ink,
				backgroundColor: theme.orange,
				borderWidth: theme.borderWidth,
				fill: true,
				// Hard angles rather than a soft spline.
				tension: 0,
				pointRadius: 0,
			}],
		},
		options: { ...baseOptions, scales: axes(), plugins: { datalabels: { display: false } } },
	});
}

async function renderLatencyChart() {
	const points = await fetchJSON("/api/stats/latency");
	new Chart(document.getElementById("latencyChart"), {
		type: "bar",
		data: {
			labels: points.map((p) => shortResolverName(p.resolver)),
			datasets: [barDataset("Avg latency (ms)", points.map((p) => Math.round(p.value * 1000)), theme.blue)],
		},
		options: { ...baseOptions, scales: axes(), plugins: { datalabels: barLabels } },
	});
}

renderSuccessChart().catch((err) => renderChartError("successChart", err));
renderRequestsChart().catch((err) => renderChartError("requestsChart", err));
renderLatencyChart().catch((err) => renderChartError("latencyChart", err));

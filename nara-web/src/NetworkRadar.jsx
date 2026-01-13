// nara - Network Radar / Fantasy Map

import { useState, useEffect, useRef } from 'preact/hooks';
import * as d3 from 'd3';

export function NetworkRadar() {
  const [mapData, setMapData] = useState({ nodes: [], server: '' });
  const [tooltip, setTooltip] = useState(null);
  const svgRef = useRef(null);

  useEffect(() => {
    const fetchMap = () => {
      fetch("/network/map")
        .then(res => res.json())
        .then(data => setMapData(data))
        .catch(() => {});
    };

    fetchMap();
    const interval = setInterval(fetchMap, 10000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    if (!svgRef.current || mapData.nodes.length === 0) return;

    const svg = d3.select(svgRef.current);
    const width = 900;
    const height = 500;

    svg.selectAll("*").remove();

    // Whimsical fantasy map colors
    const mapBg = "#f5ebe0"; // warm beige
    const borderColor = "#8b7355"; // brown
    const textColor = "#5d4e37"; // dark brown

    // Background
    svg.append("rect")
      .attr("width", width)
      .attr("height", height)
      .attr("fill", mapBg);

    // Add paper texture with subtle noise
    for (let i = 0; i < 80; i++) {
      svg.append("circle")
        .attr("cx", Math.random() * width)
        .attr("cy", Math.random() * height)
        .attr("r", Math.random() * 1.5)
        .attr("fill", "#d4c4b0")
        .attr("opacity", Math.random() * 0.15);
    }

    // Process nodes
    const visibleNodes = mapData.nodes.filter(n => n.online);
    const nodesWithCoords = visibleNodes.filter(n => n.coordinates?.x !== undefined);
    const nodesWithoutCoords = visibleNodes.filter(n => !n.coordinates || n.coordinates.x === undefined);

    // Calculate bounds for positioning
    let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
    nodesWithCoords.forEach(node => {
      minX = Math.min(minX, node.coordinates.x);
      maxX = Math.max(maxX, node.coordinates.x);
      minY = Math.min(minY, node.coordinates.y);
      maxY = Math.max(maxY, node.coordinates.y);
    });

    if (!isFinite(minX) || minX === maxX) { minX = -1; maxX = 1; }
    if (!isFinite(minY) || minY === maxY) { minY = -1; maxY = 1; }

    const padding = 0.15;
    const rangeX = maxX - minX;
    const rangeY = maxY - minY;
    minX -= rangeX * padding; maxX += rangeX * padding;
    minY -= rangeY * padding; maxY += rangeY * padding;

    // Scale to fit on map
    const mapMargin = 60;
    const xScale = d3.scaleLinear().domain([minX, maxX]).range([mapMargin, width - mapMargin]);
    const yScale = d3.scaleLinear().domain([minY, maxY]).range([mapMargin, height - mapMargin]);

    // Draw cute little house for each nara
    const drawHouse = (node, x, y) => {
      const group = svg.append("g")
        .attr("class", "map-house")
        .style("cursor", "pointer");

      const isSelf = node.is_self;
      const houseColor = node.aura || (isSelf ? "#ffd93d" : "#d4a574");
      const roofColor = isSelf ? "#ff7ac3" : "#a67c52";

      // Shadow
      group.append("ellipse")
        .attr("cx", x)
        .attr("cy", y + 18)
        .attr("rx", 12)
        .attr("ry", 4)
        .attr("fill", "#00000015");

      // House body - cute rounded rectangle
      group.append("rect")
        .attr("x", x - 10)
        .attr("y", y - 5)
        .attr("width", 20)
        .attr("height", 18)
        .attr("rx", 2)
        .attr("fill", houseColor)
        .attr("stroke", borderColor)
        .attr("stroke-width", 1.5);

      // Roof - triangle
      group.append("path")
        .attr("d", `M${x - 14},${y - 4} L${x},${y - 16} L${x + 14},${y - 4} Z`)
        .attr("fill", roofColor)
        .attr("stroke", borderColor)
        .attr("stroke-width", 1.5);

      // Door
      group.append("rect")
        .attr("x", x - 3)
        .attr("y", y + 5)
        .attr("width", 6)
        .attr("height", 8)
        .attr("rx", 1)
        .attr("fill", "#8b7355")
        .attr("stroke", borderColor)
        .attr("stroke-width", 1);

      // Window
      group.append("rect")
        .attr("x", x - 8)
        .attr("y", y + 1)
        .attr("width", 4)
        .attr("height", 4)
        .attr("fill", "#b0d4ff")
        .attr("stroke", borderColor)
        .attr("stroke-width", 1);

      // Special marker for self
      if (isSelf) {
        group.append("text")
          .attr("x", x + 18)
          .attr("y", y - 8)
          .attr("text-anchor", "start")
          .attr("font-size", "16px")
          .text("★");
      }

      // Name label
      group.append("text")
        .attr("x", x)
        .attr("y", y + 28)
        .attr("text-anchor", "middle")
        .attr("fill", textColor)
        .attr("font-family", "'Comic Sans MS', cursive, sans-serif")
        .attr("font-size", "11px")
        .attr("font-weight", "600")
        .text(node.name.length > 10 ? node.name.slice(0, 9) + '..' : node.name);

      // Hover and click events
      group.on("mouseenter", function(event) {
        setTooltip({
          x: x + 25,
          y: y - 15,
          node
        });
      })
      .on("mousemove", function(event) {
        setTooltip({
          x: x + 25,
          y: y - 15,
          node
        });
      })
      .on("mouseleave", function() {
        setTooltip(null);
      })
      .on("click", function() {
        window.location.href = `/nara/${encodeURIComponent(node.name)}`;
      });
    };

    // Draw positioned houses
    nodesWithCoords.forEach(node => {
      drawHouse(node, xScale(node.coordinates.x), yScale(node.coordinates.y));
    });

    // Draw unpositioned houses in a cute cluster
    nodesWithoutCoords.forEach((node, i) => {
      const cols = Math.ceil(Math.sqrt(nodesWithoutCoords.length));
      const row = Math.floor(i / cols);
      const col = i % cols;
      const spacing = 50;
      const startX = width - (cols * spacing) - 40;
      const startY = 60;
      drawHouse(node, startX + col * spacing, startY + row * spacing);
    });

    // Decorative elements - scattered flowers
    const flowers = ['🌸', '🌼', '🌺', '💐', '🌻'];
    for (let i = 0; i < 8; i++) {
      svg.append("text")
        .attr("x", 20 + Math.random() * (width - 40))
        .attr("y", 20 + Math.random() * (height - 40))
        .attr("font-size", "18px")
        .attr("opacity", 0.4)
        .text(flowers[Math.floor(Math.random() * flowers.length)]);
    }

    // Map title
    svg.append("text")
      .attr("x", 20)
      .attr("y", 30)
      .attr("fill", textColor)
      .attr("font-family", "'Comic Sans MS', cursive, sans-serif")
      .attr("font-size", "18px")
      .attr("font-weight", "bold")
      .text("Network Map");

    // Online count
    svg.append("text")
      .attr("x", 20)
      .attr("y", height - 20)
      .attr("fill", textColor)
      .attr("font-family", "'Comic Sans MS', cursive, sans-serif")
      .attr("font-size", "13px")
      .text(`${visibleNodes.length} houses online`);

  }, [mapData]);

  return (
    <div className="fantasy-map-container">
      <svg ref={svgRef} width="900" height="500"></svg>
      {tooltip && (
        <div className="map-tooltip" style={{ left: tooltip.x, top: tooltip.y }}>
          <div className="tooltip-name">{tooltip.node.name}</div>
          {tooltip.node.is_self && <div className="tooltip-you">⭐ You are here!</div>}
          <div className="tooltip-status">Online</div>
          {tooltip.node.rtt_to_us !== undefined && (
            <div className="tooltip-ping">{tooltip.node.rtt_to_us.toFixed(1)}ms away</div>
          )}
        </div>
      )}
    </div>
  );
}

export function MapView() {
  return (
    <div className="map-view">
      <NetworkRadar />
    </div>
  );
}

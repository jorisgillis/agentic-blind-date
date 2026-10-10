// ============================================================
//  The Match Edge Trophy — Agentic Blind Date
//  Award for the most connected participant of the evening.
//
//  Concept: the big screen's D3 graph made physical. A trunk
//  rises from the hex base and splits into two branches, each
//  ending in a "persona node" sphere. A bar bridges the two
//  nodes — the match edge — and the heart sits on that edge:
//  the match itself. Two nodes, one edge, one heart.
//
//  Workflow:
//    1. Tweak the parameters below (Customizer-friendly).
//    2. F6 to render, then File > Export > Export as STL.
//
//  Print settings (FDM, PLA):
//    - Layer height: 0.2 mm, infill 15%
//    - Supports: none needed in theory; the bar bridges ~31 mm
//      (normal FDM bridging, minor cosmetic sag possible).
//      For a perfect underside, enable tree supports for the
//      part above z = 70.
//    - The node spheres are flattened (0.8 in Z) to keep their
//      undersides printable; a small ring of "elephant skin"
//      under each node is normal and reads as texture.
//    - Size: ~62 x 62 x 97 mm at scale 1.0
//    - Two-color option: pause at z = 15 mm (top of the base)
//      and swap filament for the graph + heart.
// ============================================================

/* [Global] */
// Overall scale (1.0 = ~9.7 cm tall)
scale_factor = 1.0;

/* [Base] */
// Hexagon diameter, point to point (mm)
base_d = 62;
// Main base height (mm)
base_h = 12;
// Chamfer step on top of the base (mm)
base_rim_h = 3;
// One word per face; face 0 ("AGENTIC") is the front
base_words = ["AGENTIC", "BLIND", "DATE", "MOST", "CONNECTED", "2026"];
// Letter height (mm) — "CONNECTED" must fit its 31 mm face
base_text_size = 5.2;
// Engraving depth (mm)
text_depth = 1.2;
font_name = "Liberation Sans:style=Bold";

/* [Graph motif] */
// Trunk diameter at the base top (mm)
trunk_bottom_d = 8;
// Trunk diameter at its top (mm)
trunk_top_d = 7;
// Height where the branches split off (mm above base top)
junction_z = 52;
// Trunk top height — the heart's bottom point fuses here (mm)
trunk_top_z = 70;

/* [Nodes] */
// Node sphere diameter (mm)
node_d = 15;
// Flatten factor in Z — 1.0 = full sphere, <1 = more printable
node_flatten = 0.8;
// Horizontal distance of node centers from the axis (mm)
node_run = 23;
// Height of node centers (mm)
node_z = 82;

/* [Edge bar] */
// Bar diameter (mm) — also the literal "edge"
bar_d = 5;

/* [Heart] */
// Heart scale (heart polygon is ~36 x 38 mm at scale 1.0)
heart_scale = 0.8;
// Heart thickness (mm) — must exceed bar_d so the bar hides inside
heart_thick = 6;
// Height of the heart origin (tunes how it sits on the bar)
heart_z = 73.2;

$fn = 64; // smoothness; hex faces override with $fn = 6

// ---------- computed ----------
apothem  = base_d / 2 * cos(30);
base_top = base_h + base_rim_h;

// ---------- base (shared pattern with The Merge Trophy) ----------
module hex_base() {
    cylinder(h = base_h, d = base_d, $fn = 6);
    translate([0, 0, base_h])
        cylinder(h = base_rim_h, d = base_d - 6, $fn = 6);
}

// One engraved word on one hex face. Faces are centered at
// 90 + 60*i degrees so that word 0 faces the viewer.
module face_text(word, i) {
    angle = 90 + 60 * i;
    rotate([0, 0, angle])
        translate([0, apothem - text_depth, base_h / 2])
            rotate([90, 0, 180])
                linear_extrude(height = text_depth + 0.02)
                    text(word, size = base_text_size,
                         font = font_name,
                         halign = "center", valign = "center");
}

module trophy_base() {
    difference() {
        hex_base();
        for (i = [0 : 5])
            face_text(base_words[i], i);
    }
}

// ---------- graph motif ----------
module trunk() {
    hull() {
        translate([0, 0, base_top]) cylinder(h = 0.1, d = trunk_bottom_d);
        translate([0, 0, trunk_top_z]) cylinder(h = 0.1, d = trunk_top_d);
    }
}

// One branch: a tapered capsule from the junction on the trunk
// up to a node. Tilt is ~39 degrees from vertical.
module arm(s) {
    hull() {
        translate([0, 0, junction_z]) sphere(d = trunk_top_d + 2);
        node(s);
    }
}

// A persona node: flattened sphere at the branch tip.
// Flattening keeps the underside within printable angles.
module node(s) {
    translate([s * node_run, 0, node_z])
        scale([1, 1, node_flatten])
            sphere(d = node_d);
}

// The match edge: a bar bridging both nodes. It prints as an
// FDM bridge (~31 mm clear span) and hides inside the heart
// at the center.
module edge_bar() {
    translate([0, 0, node_z])
        rotate([0, 90, 0])
            cylinder(h = 2 * node_run - node_d + 6, d = bar_d, center = true);
}

// ---------- heart ----------
// 2D heart, point down, ~36 wide x 38 tall at scale 1.0.
module heart_2d() {
    polygon([
        [0, -13], [7, -6], [11, -1], [11, 4], [8, 9.5],
        [4.5, 11.5], [1.5, 10.5], [0, 8.5], [-1.5, 10.5],
        [-4.5, 11.5], [-8, 9.5], [-11, 4], [-11, -1], [-7, -6]
    ]);
}

// The heart sits on the bar (fully fused) and its bottom point
// drops into the trunk top, so the whole crown is one solid.
// The lobes also kiss the branches around their widest point,
// which braces the heart laterally while printing.
module heart_top() {
    translate([0, 0, heart_z])
        rotate([90, 0, 0])
            linear_extrude(height = heart_thick, center = true)
                scale(heart_scale)
                    heart_2d();
}

// ---------- assembly ----------
module trophy() {
    trophy_base();
    trunk();
    for (s = [-1, 1])
        arm(s);
    edge_bar();
    heart_top();
}

scale(scale_factor) trophy();

// ============================================================
//  Tuning tips:
//   - If your printer droops under the nodes, lower
//     node_flatten to 0.7 (flatter = easier overhang).
//   - If the bar's underside sags, either accept it (it is
//     hidden inside the heart's shadow) or slice with tree
//     supports from z = 70 up.
//   - Bigger stage version: scale_factor = 1.3 (~13 cm).
//   - Swap base_words[5] ("2026") for the event or edition
//     name of each meetup.
// ============================================================

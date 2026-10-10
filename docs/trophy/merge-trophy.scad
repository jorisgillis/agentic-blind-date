// ============================================================
//  The Merge Trophy — Agentic Blind Date
//  Award for the most connected participant of the evening.
//
//  Concept: two git "branch tips" rise from the hex base and
//  merge into a single trunk — a merge commit standing upright.
//  The heart on top is the blind date. The hexagonal base
//  carries one word per face.
//
//  Workflow:
//    1. Tweak the parameters below (Customizer-friendly).
//    2. F6 to render, then File > Export > Export as STL.
//
//  Print settings (FDM, PLA):
//    - Layer height: 0.2 mm
//    - Infill: 15%
//    - Supports: none needed (all overhangs <= ~31°)
//    - Size: ~62 x 62 x 92 mm at scale 1.0
//    - Two-color option: pause the print at the top of the
//      base (z = 15 mm) and swap filament for the merge motif.
// ============================================================

/* [Global] */
// Overall scale (1.0 = ~9 cm tall)
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
// Letter height (mm) — keep "CONNECTED" on its 31 mm face
base_text_size = 5.2;
// Engraving depth (mm)
text_depth = 1.2;
font_name = "Liberation Sans:style=Bold";

/* [Merge motif] */
// Branch feet distance from the center (mm)
arm_spread = 17;
// Commit-node diameter at the feet (mm)
arm_bottom_d = 11;
// Trunk diameter at the merge point (mm)
arm_top_d = 7;
// Branch convergence height above the base (mm)
stem_height = 34;

/* [Topper] */
// "heart" = the blind date; "node" = plain commit node
topper = "heart"; // [heart, node]
// Thickness of the heart (mm)
heart_thick = 5;

$fn = 64; // smoothness; hex faces override with $fn = 6

// ---------- computed ----------
apothem  = base_d / 2 * cos(30);   // inradius of the hexagon
base_top = base_h + base_rim_h;
merge_z  = base_top + stem_height;

// ---------- modules ----------
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

// A "commit node" sitting on the base: a short collar plus a
// sphere, so the equator (steepest area) is a vertical wall.
module foot(s) {
    translate([s * arm_spread, 0, base_top]) {
        cylinder(h = 2.5, d = arm_bottom_d);
        translate([0, 0, 2.5])
            sphere(d = arm_bottom_d);
    }
}

// A tapered branch from a foot up to the merge point.
// Tilt is ~31 degrees from vertical, well within FDM limits.
module arm(s) {
    hull() {
        translate([s * arm_spread, 0, base_top + arm_bottom_d / 2])
            sphere(d = arm_bottom_d - 2);
        translate([0, 0, merge_z])
            sphere(d = arm_top_d);
    }
}

// The single trunk that both branches merge into.
module trunk() {
    hull() {
        translate([0, 0, merge_z])
            sphere(d = arm_top_d);
        translate([0, 0, merge_z + 16])
            sphere(d = arm_top_d - 2);
    }
}

// 2D heart, point down, ~25 mm tall.
module heart_2d() {
    polygon([
        [0, -13], [7, -6], [11, -1], [11, 4], [8, 9.5],
        [4.5, 11.5], [1.5, 10.5], [0, 8.5], [-1.5, 10.5],
        [-4.5, 11.5], [-8, 9.5], [-11, 4], [-11, -1], [-7, -6]
    ]);
}

module heart_top() {
    // Bottom point of the heart is buried in the trunk so the
    // two parts fuse into one solid.
    translate([0, 0, merge_z + 14])
        rotate([90, 0, 0])
            linear_extrude(height = heart_thick, center = true)
                heart_2d();
}

module node_top() {
    translate([0, 0, merge_z + 16])
        sphere(d = arm_top_d + 4);
}

module trophy() {
    trophy_base();
    for (s = [-1, 1]) {
        foot(s);
        arm(s);
    }
    trunk();
    if (topper == "heart")
        heart_top();
    else
        node_top();
}

scale(scale_factor) trophy();

// ============================================================
//  Variants you can try quickly:
//   - topper = "node"          → plainer, more "git" look
//   - base_words               → change "2026" to the event name
//   - scale_factor             → 1.4 makes a ~13 cm stage trophy
//   - Two-tone: pause at z = 15 mm, swap to a contrast PLA;
//     the merge motif and heart then pop against the base.
// ============================================================

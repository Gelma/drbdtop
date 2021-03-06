/*
 *drbdtop - statistics for DRBD
 *Copyright © 2017 Hayley Swimelar and Roland Kammerer
 *
 *This program is free software; you can redistribute it and/or modify
 *it under the terms of the GNU General Public License as published by
 *the Free Software Foundation; either version 2 of the License, or
 *(at your option) any later version.
 *
 *This program is distributed in the hope that it will be useful,
 *but WITHOUT ANY WARRANTY; without even the implied warranty of
 *MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 *GNU General Public License for more details.
 *
 *You should have received a copy of the GNU General Public License
 *along with this program; if not, see <http://www.gnu.org/licenses/>.
 */

package display

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/LINBIT/drbdtop/pkg/convert"
	"github.com/LINBIT/drbdtop/pkg/resource"
	"github.com/LINBIT/drbdtop/pkg/update"

	"github.com/LINBIT/termui"
)

type win int

const (
	insync win = iota
	status
	detailedstatus
	dmesgw
)

type uiGauge struct {
	p *termui.Par
	g *termui.Gauge
}

type detailView struct {
	grid           *termui.Grid
	header, footer *termui.Par
	oldselres      string
	selres         string
	volGauges      map[string]uiGauge // oos view
	status         *termui.Par        // status & dmesg view
	window         win
	// constantly updating status leads to flickering, especially for the dmesg output
	scratch string // that is where you prepare you status
	buf     string // the buffer that is set to scratch if scratch != buf
}

const txtUnconfigured = "This resource is Unconfigured, no further information available."

func NewDetailView() *detailView {
	d := detailView{
		grid:      nil,
		volGauges: make(map[string]uiGauge),
	}

	d.header = termui.NewPar("")
	d.header.Height = 1
	d.header.TextFgColor = termui.ColorDefault
	d.header.TextBgColor = termui.ColorDefault
	d.header.Border = false

	d.status = termui.NewPar("")
	d.status.Height = 3
	d.status.TextFgColor = termui.ColorDefault
	d.status.TextBgColor = termui.ColorDefault

	d.footer = termui.NewPar("q: back | s: status | d: detailed status | m: dmesg | i: inSync")
	d.footer.Height = 1
	d.footer.TextFgColor = termui.ColorDefault
	d.footer.TextBgColor = termui.ColorDefault
	d.footer.Border = false
	d.window = status

	return &d
}

func (d *detailView) printRes(r *update.ByRes) {
	d.scratch += fmt.Sprintf("%s: %s: (Overall danger score: %d) ", colDefault("Resource", true), r.Res.Name, r.Danger)

	if r.Res.Suspended != "no" {
		d.scratch += fmt.Sprintf("(Suspended)")
	}

	d.scratch += fmt.Sprintf("\n")
}

func (dv *detailView) printLocalDisk(r *update.ByRes) {
	dv.scratch += fmt.Sprintf(" %s(%s):\n", colDefault("Local Disc", true), r.Res.Role)

	d := r.Device

	var keys []string
	for k := range d.Volumes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := d.Volumes[k]
		dv.scratch += fmt.Sprintf("  volume %s (/dev/drbd%s):", k, v.Minor)
		dState := v.DiskState

		if dState == "UpToDate" {
			dState = colGreen(dState, false)
		} else {
			dState = colRed(dState, true)
		}
		dv.scratch += fmt.Sprintf(" %s", dState)

		dv.scratch += fmt.Sprintf("(%s)", v.DiskHint)

		if dv.window == detailedstatus {
			if v.Blocked != "no" {
				dv.scratch += fmt.Sprintf(" Blocked: %s ", d.Volumes[k].Blocked)
			}

			if v.ActivityLogSuspended != "no" {
				dv.scratch += fmt.Sprintf(" Activity Log Suspended: %s ", d.Volumes[k].Blocked)
			}

			dv.scratch += fmt.Sprintf("\n")
			dv.scratch += fmt.Sprintf("    size: %s total-read:%s read/Sec:%s total-written:%s written/Sec:%s ",
				convert.KiB2Human(float64(v.Size)),
				convert.KiB2Human(float64(v.ReadKiB.Total)), convert.KiB2Human(v.ReadKiB.PerSecond),
				convert.KiB2Human(float64(v.WrittenKiB.Total)), convert.KiB2Human(v.WrittenKiB.PerSecond))
		}
		dv.scratch += fmt.Sprintf("\n")
	}
}

func (d *detailView) printConn(c *resource.Connection) {
	d.scratch += fmt.Sprintf("%s", colDefault(fmt.Sprintf(" Connection to %s", c.ConnectionName), true))

	d.scratch += fmt.Sprintf("(%s):", c.Role)

	status := c.ConnectionStatus
	if status == "Connected" {
		status = colGreen(status, false)
	} else {
		status = colRed(status, true)
	}
	d.scratch += fmt.Sprintf(" %s", status)
	d.scratch += fmt.Sprintf("(%s)", c.ConnectionHint)

	if c.Congested != "no" {
		d.scratch += fmt.Sprintf(" Congested ")
	}

	d.scratch += fmt.Sprintf("\n")
}

func (dv *detailView) printPeerDev(r *update.ByRes, conn string) {
	d := r.PeerDevices[conn]

	var keys []string
	for k := range d.Volumes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := d.Volumes[k]
		dv.scratch += fmt.Sprintf("  volume %s: ", k)

		if v.ResyncSuspended != "no" {
			dv.scratch += fmt.Sprintf(" ResyncSuspended:%s ", v.ResyncSuspended)
		}
		dv.scratch += fmt.Sprintf("\n")

		if v.ReplicationStatus != "Established" {
			dv.scratch += fmt.Sprintf("   Replication:%s", v.ReplicationStatus)
			dv.scratch += fmt.Sprintf("(%s)", v.ReplicationHint)
		}

		if strings.HasPrefix(v.ReplicationStatus, "Sync") {
			dv.scratch += fmt.Sprintf(" %.1f%% remaining",
				(float64(v.OutOfSyncKiB.Current)/float64(r.Device.Volumes[k].Size))*100)
		}

		status := v.DiskState
		if status == "UpToDate" {
			status = colGreen(status, false)
		} else {
			status = colRed(status, true)
		}
		dv.scratch += fmt.Sprintf("   %s", status)
		dv.scratch += fmt.Sprintf("(%s)", v.DiskHint)

		dv.scratch += fmt.Sprintf("\n")

		if dv.window == detailedstatus {
			dv.scratch += fmt.Sprintf("   Sent: total:%s Per/Sec:%s\n",
				convert.KiB2Human(float64(v.SentKiB.Total)), convert.KiB2Human(v.SentKiB.PerSecond))

			dv.scratch += fmt.Sprintf("   Received: total:%s Per/Sec:%s\n",
				convert.KiB2Human(float64(v.ReceivedKiB.Total)), convert.KiB2Human(v.ReceivedKiB.PerSecond))

			dv.scratch += fmt.Sprintf("   OutOfSync: current:%s average:%s min:%s max:%s\n",
				convert.KiB2Human(float64(v.OutOfSyncKiB.Current)),
				convert.KiB2Human(float64(v.OutOfSyncKiB.Avg)),
				convert.KiB2Human(float64(v.OutOfSyncKiB.Min)),
				convert.KiB2Human(float64(v.OutOfSyncKiB.Max)))

			dv.scratch += fmt.Sprintf("   PendingWrites: current:%s average:%s min:%s max:%s\n",
				fmt.Sprintf("%.1f", float64(v.PendingWrites.Current)),
				fmt.Sprintf("%.1f", float64(v.PendingWrites.Avg)),
				fmt.Sprintf("%.1f", float64(v.PendingWrites.Min)),
				fmt.Sprintf("%.1f", float64(v.PendingWrites.Max)))

			dv.scratch += fmt.Sprintf("   UnackedWrites: current:%s average:%s min:%s max:%s\n",
				fmt.Sprintf("%.1f", float64(v.UnackedWrites.Current)),
				fmt.Sprintf("%.1f", float64(v.UnackedWrites.Avg)),
				fmt.Sprintf("%.1f", float64(v.UnackedWrites.Min)),
				fmt.Sprintf("%.1f", float64(v.UnackedWrites.Max)))

			dv.scratch += fmt.Sprintf("\n")
		}
	}
}

func (d *detailView) printByRes(r *update.ByRes) {
	d.scratch = ""
	d.printRes(r)
	if r.Res.Unconfigured {
		d.scratch += fmt.Sprintf("\n%s\n", txtUnconfigured)
		d.UpdateStatusFromScratch()
		return
	}

	d.printLocalDisk(r)
	d.scratch += fmt.Sprintf("\n")

	var connKeys []string
	for j := range r.Connections {
		connKeys = append(connKeys, j)
	}
	sort.Strings(connKeys)

	for _, conn := range connKeys {
		if c, ok := r.Connections[conn]; ok {
			d.printConn(c)

			if _, ok := r.PeerDevices[conn]; ok {
				d.printPeerDev(r, conn)
			}
			d.scratch += fmt.Sprintf("\n")
		}
	}

	d.UpdateStatusFromScratch()
}

func (d *detailView) UpdateStatusFromScratch() {
	if d.scratch != d.buf {
		d.buf = d.scratch
		d.status.Text = d.scratch
	}
}

func (d *detailView) UpdateDmesg() {
	d.scratch = fmt.Sprintf("%s %s:\n", colDefault("Dmesg output for resource", true), colDefault(d.selres, true))

	lines, err := dmesg(d.selres)
	if err != nil {
		d.status.Text = err.Error()
		return
	}

	start := 0
	space := d.status.Height - 2
	if space < len(lines) {
		start = len(lines) - space

	}
	for i := start; i < len(lines); i++ {
		d.scratch += lines[i] + "\n"
	}

	d.UpdateStatusFromScratch()

	d.oldselres = d.selres
}

func (d *detailView) UpdateStatus() {
	db.RLock()
	defer db.RUnlock()
	for _, rname := range db.keys {
		if rname == d.selres {
			res := db.buf[rname]
			d.printByRes(&res)
		}
	}

	d.oldselres = d.selres
}

func (d *detailView) UpdateInSync() {
	db.RLock()
	defer db.RUnlock()

	for _, rname := range db.keys {
		if rname != d.selres {
			continue
		}
		res := db.buf[rname]
		dev := res.Device
		vols := dev.Volumes
		if d.selres != d.oldselres {
			/* THINK or empty the old one? */
			d.volGauges = make(map[string]uiGauge)
		}
		for k, v := range vols {
			if _, ok := d.volGauges[k]; !ok {
				g := termui.NewGauge()
				g.Height = 3
				g.BorderLabel = "In Sync"
				g.BorderLabelFg = termui.ColorGreen

				ps := fmt.Sprintf("Vol %s (/dev/drbd%s)", k, v.Minor)
				p := termui.NewPar(ps)
				p.Height = 3
				var vg uiGauge
				vg.g, vg.p = g, p
				d.volGauges[k] = vg
			}

			var oos, nrPeerDevs uint64
			for _, pdev := range res.PeerDevices {
				pvol := pdev.Volumes[k]
				oos += pvol.OutOfSyncKiB.Current
				nrPeerDevs++
			}

			// oosp is oos over *all* peers, sizes are (roughly) the same, so multiply v.Size by nrPeerDevs, to get sane percentage
			oosp := int(float64(oos*100) / float64(v.Size*nrPeerDevs))
			inSyncp := 100 - oosp
			if inSyncp == 100 && oos > 0 {
				inSyncp = 99 // make it visable that something is oos
			}
			d.volGauges[k].g.Percent = inSyncp
		}
	}
	d.oldselres = d.selres
}

func (d *detailView) updateContent() {
	switch d.window {
	case insync:
		d.UpdateInSync()
	case status, detailedstatus:
		d.UpdateStatus()
	case dmesgw:
		d.UpdateDmesg()
	default:
		panic("window")
	}

}

func (d *detailView) updateGUI(updateContent bool) {
	d.header.Text = drbdtopversion + " - Details for " + d.selres
	if updateContent {
		d.updateContent()
	}
	d.grid = termui.NewGrid()
	d.grid.AddRows(
		termui.NewRow(
			termui.NewCol(12, 0, d.header)))

	var heights int

	switch d.window {
	case insync:
		for _, uig := range d.volGauges {
			d.grid.AddRows(
				termui.NewRow(
					termui.NewCol(3, 0, uig.p),
					termui.NewCol(9, 0, uig.g)))
		}
		heights = len(d.volGauges)*3 + d.header.Height + d.footer.Height
	case status, detailedstatus, dmesgw:
		statusheight := termui.TermHeight() - d.header.Height - d.footer.Height
		d.status.Height = statusheight
		d.grid.AddRows(
			termui.NewRow(
				termui.NewCol(12, 0, d.status)))
		heights = d.status.Height + d.header.Height + d.footer.Height
	default:
		panic("window")
	}

	spacerheight := termui.TermHeight() - heights
	if spacerheight > 0 {
		s := termui.NewPar("")
		s.Border = false
		s.Height = termui.TermHeight() - heights
		fmt.Fprintln(os.Stderr, s.Height)

		d.grid.AddRows(
			termui.NewRow(
				termui.NewCol(12, 0, s)))
	}

	d.grid.AddRows(
		termui.NewRow(
			termui.NewCol(12, 0, d.footer)))

	switchDisp(d.grid)
}

// The public one, where we really want to update everything
func (d *detailView) UpdateGUI() {
	d.updateGUI(true)
}

func (d *detailView) setWindow(e termui.Event) {
	k, _ := e.Data.(termui.EvtKbd)
	old := d.window
	switch k.KeyStr {
	case "i":
		d.window = insync
	case "s":
		d.window = status
	case "d":
		d.window = detailedstatus
	case "m":
		d.window = dmesgw
	}

	if old != d.window {
		d.buf = ""
		d.updateGUI(true)
	}
}

func (d *detailView) Update() {
	// TODO: move that switch bodies to functions
	switch d.window {
	case insync:
		was := len(d.volGauges)
		d.UpdateInSync()
		is := len(d.volGauges)
		if was != is {
			d.updateGUI(false)
		} else {
			var keys []string
			for k := range d.volGauges {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				uig := d.volGauges[k]
				termui.Render(uig.p, uig.g)
			}
		}
	case status, detailedstatus:
		d.UpdateStatus()
		termui.Render(d.status)
	case dmesgw:
		d.UpdateDmesg()
		termui.Render(d.status)
	default:
		panic("window")
	}
}

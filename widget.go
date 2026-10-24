package mygo

import "encoding/json"

// WidgetRow is one line of the home screen widget.
type WidgetRow struct {
	// ID is what Widget.OnAction receives when the row is tapped.
	ID string
	// Label is the line's main text.
	Label string
	// Subtitle is the smaller text on the right.
	Subtitle string
}

// WidgetContent is what the home screen widget shows: a title and up to four
// rows. A richer layout needs its own AppWidgetProvider in the app.
type WidgetContent struct {
	Title string
	Rows  []WidgetRow
}

// WidgetModule drives the home screen widget.
type WidgetModule struct{}

type widgetJSON struct {
	Title string      `json:"title"`
	Rows  []widgetRow `json:"rows"`
}

type widgetRow struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Subtitle string `json:"subtitle"`
}

// Widget is the home screen widget.
var Widget = &WidgetModule{}

// Set replaces what the widget shows, on every instance on the home screen.
// Only the first four rows are used. The app can call it as often as its state
// changes; the system redraws at the next frame.
func (m *WidgetModule) Set(content WidgetContent) error {
	needsApp("Widget.Set")
	rows := content.Rows
	if len(rows) > 4 {
		rows = rows[:4]
	}
	payload := widgetJSON{Title: content.Title}
	for _, row := range rows {
		payload.Rows = append(payload.Rows, widgetRow{ID: row.ID, Label: row.Label, Subtitle: row.Subtitle})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var updateErr error
	onMain(func() { updateErr = backend().Mobile().UpdateWidget(string(encoded)) })
	return updateErr
}

// Clear leaves the widget with its default title and no rows.
func (m *WidgetModule) Clear() error {
	return m.Set(WidgetContent{})
}

// OnAction receives the ID of a tapped row, on the UI thread.
func (m *WidgetModule) OnAction(fn func(id string)) (off func()) {
	return mobileEvents.widgetActions.add(fn, false)
}

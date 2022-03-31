package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-ping/ping"
	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v3"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// CFGFILE - path to config file
const CFGFILE string = "config.yml"

// Config struct
type Config struct {
	BotToken      string          `yaml:"bot_token"`
	WebhookURL    string          `yaml:"webhook_url"`
	ListenPort    string          `yaml:"listen_port"`
	ReportChannel int64           `yaml:"report_channel"`
	Admin         int64           `yaml:"admin"`
	Users         map[int64]*User `yaml:"users"`
	InkoToolsAPI  string          `yaml:"inkotools_api_url"`
	DebugMode     bool            `yaml:"debug"`
}

// User struct
type User struct {
	Name string `yaml:"name"`
	Mode string `yaml:"-"`
}

// CFG - config object
var CFG Config

// TPL - templates object
var TPL *template.Template

// Bot - bot object
var Bot *tgbotapi.BotAPI

// Pingers - map of active pingers, key is uid
var Pingers map[int64]ping.Pinger

// Switch type
type Switch struct {
	IP       string `mapstructure:"ip"`
	Location string `mapstructure:"location"`
	MAC      string `mapstructure:"mac"`
	Model    string `mapstructure:"model"`
	Status   bool   `mapstructure:"status"`
}

// Port type
type Port struct {
	Port          int    `mapstructure:"port"`
	Type          string `mapstructure:"type"`
	State         bool   `mapstructure:"state"`
	Speed         string `mapstructure:"speed"`
	Link          bool   `mapstructure:"link"`
	Status        string `mapstructure:"status"`
	Learning      bool   `mapstructure:"learning"`
	Autodowngrade bool   `mapstructure:"autodowngrade"`
	Description   string `mapstructure:"desc"`
	Cable         []Pair `mapstructure:"cable"`
}

// Pair type - pair in cable
type Pair struct {
	Pair  int    `mapstructure:"pair"`
	State string `mapstructure:"state"`
	Len   int    `mapstructure:"len"`
}

// PortBandwidth limits type
type PortBandwidth struct {
	RX uint `mapstructure:"rx"`
	TX uint `mapstructure:"tx"`
}

// PortCounters type
type PortCounters struct {
	TotalRX  uint        `mapstructure:"rx_total"`
	TotalTX  uint        `mapstructure:"tx_total"`
	SpeedRX  uint        `mapstructure:"rx_speed"`
	SpeedTX  uint        `mapstructure:"tx_speed"`
	ErrorsRX []PortError `mapstructure:"rx_errors"`
	ErrorsTX []PortError `mapstructure:"tx_errors"`
}

// PortError type
type PortError struct {
	Name  string `mapstructure:"name"`
	Count int    `mapstructure:"count"`
}

// PortVlan type
type PortVlan struct {
	Port     int   `mapstructure:"port"`
	Untagged []int `mapstructure:"untagged"`
	Tagged   []int `mapstructure:"tagged"`
}

// PortMac type
type PortMac struct {
	Port   int    `mapstructure:"port"`
	VlanID int    `mapstructure:"vid"`
	Mac    string `mapstructure:"mac"`
}

// PortACL type
type PortACL struct {
	Port      int    `mapstructure:"port"`
	ProfileID int    `mapstructure:"profile_id"`
	AccessID  int    `mapstructure:"access_id"`
	IP        string `mapstructure:"ip"`
	Mask      string `mapstructure:"mask"`
	Mode      string `mapstructure:"mode"`
}

// IPCalc type
type IPCalc struct {
	IP      string `mapstructure:"ip"`
	Mask    string `mapstructure:"mask"`
	Gateway string `mapstructure:"gateway"`
	Prefix  int    `mapstructure:"prefix"`
}

// ARPEntry type
type ARPEntry struct {
	IP     string `mapstructure:"ip"`
	Mac    string `mapstructure:"mac"`
	VlanID int    `mapstructure:"vid"`
	State  bool   `mapstructure:"state"`
}

// DBSearch type
type DBSearch struct {
	Data []Switch `mapstructure:"data"`
	Meta struct {
		Entries struct {
			Current int `mapstructure:"current"`
			PerPage int `mapstructure:"per_page"`
			Total   int `mapstructure:"total"`
		} `mapstructure:"entries"`
		Pages struct {
			Current int `mapstructure:"current"`
			Total   int `mapstructure:"total"`
		} `mapstructure:"pages"`
	} `mapstructure:"meta"`
}

// XCHAR - unicode symbol X
const XCHAR string = "\xE2\x9D\x8C"

// VCHAR - unicode symbol V
const VCHAR string = "\xE2\x9C\x85"

// FAILCHAR - unicode symbol crossed circle
const FAILCHAR string = "\xF0\x9F\x9A\xAB"

// OKCHAR - unicode symbol OK
const OKCHAR string = "\xF0\x9F\x86\x97"

// UPCHAR - unicode symbol UP
const UPCHAR string = "\xF0\x9F\x86\x99"

// WARNCHAR - unicode symbol !!
const WARNCHAR string = "\xE2\x80\xBC"

// ColorReset - ANSI color
const ColorReset string = "\033[0m"

// ColorRed - ANSI color
const ColorRed string = "\033[31m"

// ColorGreen - ANSI color
const ColorGreen string = "\033[32m"

// ColorYellow - ANSI color
const ColorYellow string = "\033[33m"

// ColorBlue - ANSI color
const ColorBlue string = "\033[34m"

// ColorPurple - ANSI color
const ColorPurple string = "\033[35m"

// ColorCyan - ANSI color
const ColorCyan string = "\033[36m"

// ColorWhite - ANSI color
const ColorWhite string = "\033[37m"

// HELPUSER - help string for user
const HELPUSER string = `
<b>Available commands:</b>
/help - print this help
/raw [args] - switch to raw command mode (default)
/report [args] - switch to feedback mode
/search [args] - switch to search mode
/ping [args] - switch to ping mode

<code>args</code> - optional commands, which can be executed immediately, like you are already in this mode.

<b>Raw mode (default)</b>
In this mode bot try to parse raw commands:

<code>IP</code> - depending on <b><i>IP</i></b>, get switch summary or get client ip address summary (ip, mask, gateway, prefix)

<code>IP PORT</code> - get short switch and short port summary with additional callback buttons:

<code>full/short</code> - switch between full and short port summary
<code>refresh</code> - update information in the same message
<code>clear</code> - clear port counters and refresh
<code>repeat</code> - send new message with updated information

<b><i>IP</i></b> can be in short or full format (e.g. <code>59.75</code> and <code>192.168.59.75</code> are equal)
For client's public ip you must specify address in full format.

<b>Report mode</b>
In this mode you can send bug reports or suggestions.
All messages will be redirected to special reports channel. You can also send screenshots or other media.

<b>Search mode</b>
In this mode you can search switches by mac, model or location.
Results will be paginated. Use callback buttons to navigate between pages: first, previous, next, last. 

<b>Ping mode</b>
In this mode you can ping different hosts.
Only one host at time. If you send a new host, previous pinger will be stopped.

`

// HELPADMIN - help string for admin
const HELPADMIN string = `
<code>list</code> - list authorized users
<code>add ID [NAME]</code> - add user with id <b><i>ID</i></b> and optional mark with comment <b><i>NAME</i></b>
<code>del ID</code> - delete user with id <b><i>ID</i></b>
<code>send ID TEXT</code> - send message <b><i>TEXT</i></b> to user with id <b><i>ID</i></b>
<code>broadcast TEXT</code> - send broadcast message <b><i>TEXT</i></b> 
<code>reload</code> - reload configuration from file
`

// HELPER FUNCTIONS

// check if uid is in users map
func userIsAuthorized(id int64) bool {
	_, ok := CFG.Users[id]
	return ok
}

// search int in list of int
func intInList(val int, lst []int) bool {
	sort.Ints(lst)
	idx := sort.SearchInts(lst, val)
	return !((idx == len(lst)) || (val != lst[idx]))
}

// split first arg from args
func splitArgs(args string) (first string, other string) {
	a := strings.SplitN(args, " ", 2)
	if len(a) < 2 {
		return a[0], ""
	}
	return a[0], strings.TrimSpace(a[1])
}

// split last arg from args
func splitLast(args string) (before string, last string) {
	i := strings.LastIndex(args, " ")
	return args[:i], args[i+1:]
}

// convert x.x --> 192.168.x.x, return empty string on invalid ip
func fullIP(ip string, isSwitch bool) string {
	octet := `[0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5]`
	reFull, _ := regexp.Compile(`^((` + octet + `)\.){3}(` + octet + `)$`)
	reShort, _ := regexp.Compile(`^(` + octet + `)\.(` + octet + `)$`)
	reSwitch, _ := regexp.Compile(`^192\.168\.(4[79]|5[7-9]|60)\.(` + octet + `)$`)
	if reShort.MatchString(ip) {
		ip = "192.168." + ip
	}
	if isSwitch && reSwitch.MatchString(ip) || !isSwitch && reFull.MatchString(ip) {
		return ip
	}
	return ""
}

// print error in message
func fmtErr(e string) string {
	return "\n<b>ERROR</b>" + WARNCHAR + "\n<code>" + e + "</code>\n"
}

// print object formatted with template
func fmtObj(obj interface{}, tpl string) string {
	var buf bytes.Buffer
	TPL.ExecuteTemplate(&buf, tpl, obj)
	return buf.String()
}

// print bytes in human readable format
func fmtBytes(bytes uint, toBits bool) string {
	var ratio float64 = 1024
	units := [5]string{"B", "KB", "MB", "GB", "TB"}
	if toBits {
		ratio = 1000
		units = [5]string{"bit", "Kbit", "Mbit", "Gbit", "Tbit"}
		bytes *= 8
	}
	res := float64(bytes)
	i := 0
	for ; res >= ratio && i < len(units); i++ {
		res /= ratio
	}
	return fmt.Sprintf("%.2f %s", res, units[i])
}

// print rounded duration if it is less than 100s.
func fmtRTT(d time.Duration) string {
	scale := 100 * time.Second
	// look for the max scale that is smaller than d
	for scale > d {
		scale = scale / 10
	}
	return d.Round(scale / 100).String()
}

// debug log
func logDebug(msg string) {
	if CFG.DebugMode {
		log.Printf("[%sDEBUG%s] %s", ColorCyan, ColorReset, msg)
	}
}

// info log
func logInfo(msg string) {
	log.Printf("[%sINFO%s] %s", ColorGreen, ColorReset, msg)
}

// warning log
func logWarning(msg string) {
	log.Printf("[%sWARNING%s] %s", ColorYellow, ColorReset, msg)
}

// error log
func logError(msg string) {
	log.Printf("[%sERROR%s] %s", ColorRed, ColorReset, msg)
}

// print timestamp
func printUpdated() string {
	t := time.Now().Format("2006-01-02 15:04:05")
	return fmt.Sprintf("\n<i>Updated:</i> <code>%s</code>", t)
}

// MAIN FUNCTIONS

// init telegram bot
func initBot() tgbotapi.UpdatesChannel {
	var err error
	Bot, err = tgbotapi.NewBotAPI(CFG.BotToken)
	if err != nil {
		log.Panic(err)
	}
	// Bot.Debug = CFG.DebugMode
	logInfo(fmt.Sprintf("[init] Authorized on bot account %s", Bot.Self.UserName))

	whInfo, _ := Bot.GetWebhookInfo()
	logDebug(fmt.Sprintf("[init] Got webhook info: %v", whInfo.URL))
	// check webhook is set
	if whInfo.URL != CFG.WebhookURL+Bot.Token {
		logDebug(fmt.Sprintf("[init] New webhook: %s", CFG.WebhookURL+Bot.Token))
		wh, _ := tgbotapi.NewWebhook(CFG.WebhookURL + Bot.Token)
		_, err := Bot.Request(wh)
		if err != nil {
			log.Panic(err)
		}
	}
	// init pingers
	Pingers = make(map[int64]ping.Pinger)
	// serve http
	go http.ListenAndServe(":"+CFG.ListenPort, nil)
	updates := Bot.ListenForWebhook("/" + Bot.Token)
	logInfo(fmt.Sprintf("[init] Listening on port %s", CFG.ListenPort))
	return updates
}

// load config from file
func loadConfig() error {
	data, err := ioutil.ReadFile(CFGFILE)
	if err != nil {
		logError(fmt.Sprintf("[config] Read file failed: %v", err))
		return err
	}
	err = yaml.Unmarshal(data, &CFG)
	if err != nil {
		logError(fmt.Sprintf("[config] Parse yaml failed: %v", err))
		return err
	}
	logInfo(fmt.Sprintf("[config] Loaded from %s", CFGFILE))
	// Template functions
	funcMap := template.FuncMap{
		"fmtBytes": fmtBytes,
		"fmtKbits": func(x uint) string { return fmtBytes(x*125, true) },
	}
	// load templates
	TPL, err = template.New("templates").Funcs(funcMap).ParseGlob("templates/*")
	if err != nil {
		logError(fmt.Sprintf("[template] Parse failed: %v", err))
		return err
	}
	for _, t := range TPL.Templates() {
		logDebug(fmt.Sprintf("[template] Loaded: %v", t.Name()))
	}
	return nil
}

// write config to file
func saveConfig() error {
	data, err := yaml.Marshal(&CFG)
	if err != nil {
		logError(fmt.Sprintf("[config] YAML marshal failed: %v", err))
		return err
	}
	// attach document start and end strings
	data = append([]byte("---\n"), data...)
	data = append(data, []byte("...\n")...)
	err = ioutil.WriteFile(CFGFILE, data, 0644)
	if err != nil {
		logError(fmt.Sprintf("[config] Write file failed: %v", err))
		return err
	}
	logInfo(fmt.Sprintf("[config] Saved to %s", CFGFILE))
	return nil
}

// add/delete user
func manageUser(args string, enabled bool) string {
	u, name := splitArgs(args)
	uid, err := strconv.ParseInt(u, 10, 64)
	if err != nil || uid == 0 {
		return fmtErr("Wrong uid")
	}
	var msgUser, msgAdmin string
	if enabled && !userIsAuthorized(uid) {
		CFG.Users[uid] = &User{Name: name}
		logInfo(fmt.Sprintf("[user] %d (%s) added", uid, CFG.Users[uid].Name))
		msgUser = "You are added to authorized users list."
		msgAdmin = fmt.Sprintf("User <code>%d</code> <b>%s</b> added.",
			uid, CFG.Users[uid].Name)
	} else if !enabled && userIsAuthorized(uid) {
		logInfo(fmt.Sprintf("[user] %d (%s) removed", uid, CFG.Users[uid].Name))
		msgUser = "You are removed from authorized users list."
		msgAdmin = fmt.Sprintf("User <code>%d</code> <b>%s</b> removed.",
			uid, CFG.Users[uid].Name)
		delete(CFG.Users, uid)
	} else {
		return "Nothing to do"
	}
	saveConfig()
	sendTo(uid, msgUser)
	return msgAdmin
}

// send text message with keyboard (both reply or inline) to user
func sendMessage(id int64, text string, kb interface{}) error {
	msg := tgbotapi.NewMessage(id, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = kb
	_, err := Bot.Send(msg)
	if err != nil {
		logError(fmt.Sprintf("[send] [%s] %v, msg: %+v ", CFG.Users[id].Name, err, msg))
	}
	return err
}

// edit message with inline keyboard
func editMessage(m *tgbotapi.Message, textNew string, kbNew tgbotapi.InlineKeyboardMarkup, kbReplace bool) error {
	var kb tgbotapi.InlineKeyboardMarkup
	if kbReplace {
		kb = kbNew
	} else {
		kb = *m.ReplyMarkup
	}
	msg := tgbotapi.NewEditMessageTextAndMarkup(m.Chat.ID, m.MessageID, textNew, kb)
	msg.ParseMode = tgbotapi.ModeHTML
	_, err := Bot.Send(msg)
	if err != nil {
		logError(fmt.Sprintf("[edit] %v, msg: %+v ", err, msg))
	}
	return err
}

// generate keyboard markup from matrix
func genKeyboard(matrix [][]map[string]string) tgbotapi.InlineKeyboardMarkup {
	var kb [][]tgbotapi.InlineKeyboardButton
	for _, rows := range matrix {
		var row []tgbotapi.InlineKeyboardButton
		for _, cols := range rows {
			for key, val := range cols {
				btn := tgbotapi.NewInlineKeyboardButtonData(key, val)
				row = append(row, btn)
			}
		}
		kb = append(kb, row)
	}
	return tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: kb,
	}
}

// shortcut for edit only text
func editText(m *tgbotapi.Message, txt string) error {
	return editMessage(m, txt, tgbotapi.InlineKeyboardMarkup{[][]tgbotapi.InlineKeyboardButton{}}, false)
}

// shortcut for edit text and keyboard
func editTextAndKeyboard(m *tgbotapi.Message, txt string, kb tgbotapi.InlineKeyboardMarkup) error {
	return editMessage(m, txt, kb, true)
}

// shortcut for simple text message
func sendTo(id int64, text string) error {
	return sendMessage(id, text, tgbotapi.InlineKeyboardMarkup{[][]tgbotapi.InlineKeyboardButton{}})
}

// broadcast message to all users
func broadcastSend(text string) string {
	var res string
	if text == "" {
		return fmtErr("empty message")
	}
	for uid := range CFG.Users {
		err := sendTo(uid, text)
		if err == nil {
			res += fmt.Sprintf("%d OK\n", uid)
		} else {
			res += fmt.Sprintf("%d failed: %v\n", uid, err)
		}
	}
	return res
}

// universal api request
func requestAPI(method string, endpoint string, args map[string]interface{}) (map[string]interface{}, error) {
	logDebug(fmt.Sprintf("[API %s] endpoint: %s, args: %+v", method, endpoint, args))
	var res map[string]interface{}
	if endpoint == "" {
		return res, errors.New("Empty endpoint")
	}
	// pack arguments to body
	var reqBody *bytes.Buffer
	if len(args) > 0 {
		reqData, err := json.Marshal(args)
		if err != nil {
			logError(fmt.Sprintf("[API %s] Pack args to json failed: %v, args: %+v", method, err, args))
			return res, errors.New("Packing arguments to json failed")
		}
		reqBody = bytes.NewBuffer(reqData)
	}
	// ensure that there is no double // symbols in url
	url := strings.TrimRight(CFG.InkoToolsAPI, "/") + "/" + strings.TrimLeft(endpoint, "/")
	// make request
	var req *http.Request
	var err error
	// workaround typed and untyped nil
	if reqBody == nil {
		req, err = http.NewRequest(method, url, nil)
	} else {
		req, err = http.NewRequest(method, url, reqBody)
	}
	if err != nil {
		logError(fmt.Sprintf("[API %s] Creating request object failed: %v, url: %s, body: %v", method, err, url, reqBody))
		return res, errors.New("Creating request object failed")
	}
	req.Header.Add("Content-Type", "application/json")
	// send json request to api
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logError(fmt.Sprintf("[API %s] Request failed: %v, request: %+v", method, err, req))
		return res, errors.New("API request failed")
	}
	defer resp.Body.Close()
	// parse response
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		logError(fmt.Sprintf("[API %s] Response json decode failed: %v, body: %v", method, err, resp.Body))
		return res, errors.New("API response decode failed")
	}
	// if we have no errors from api - return result
	if resp.StatusCode < 400 {
		logDebug(fmt.Sprintf("[API %s] Response: %+v", method, res))
		return res, nil
	}
	// parse errors from api
	if res["detail"] != nil {
		logError(fmt.Sprintf("[API %s] Returned %d error: %+v, request: %+v", method, resp.StatusCode, res["detail"], req))
		switch res["detail"].(type) {
		case string:
			return res, errors.New(res["detail"].(string))
		case []interface{}:
			return res, fmt.Errorf("%d", resp.StatusCode)
		}
	}
	logError(fmt.Sprintf("[API %s] Returned %d error, raw response: %+v, request: %+v", method, resp.StatusCode, res, req))
	return res, fmt.Errorf("%d", resp.StatusCode)
}

// api get request shortcut
func apiGet(endpoint string) (map[string]interface{}, error) {
	return requestAPI("GET", endpoint, map[string]interface{}{})
}

// api delete request shortcut
func apiDelete(endpoint string) (map[string]interface{}, error) {
	return requestAPI("DELETE", endpoint, map[string]interface{}{})
}

// get switch summary and format it with template
func swSummary(ip string, style string) string {
	var template string
	switch style {
	case "short":
		template = "sw.short.tmpl"
	default:
		template = "sw.tmpl"
	}
	resp, err := apiGet(fmt.Sprintf("/sw/%s/", ip))
	if err != nil {
		return fmtErr(err.Error())
	}
	// serialize data from returned map to struct
	var sw Switch
	mapstructure.Decode(resp["data"], &sw)
	res := fmtObj(sw, template)
	if !sw.Status {
		res += fmtErr("Switch is unavailable!")
	}
	return res
}

// get port summary and format it with template
func portSummary(ip string, port string, style string) string {
	var res string
	var ports []Port
	var linkUp bool
	var counters PortCounters
	var bandwidth PortBandwidth
	resp, err := apiGet(fmt.Sprintf("/sw/%s/ports/%s/", ip, port))
	if err != nil {
		return fmtErr(err.Error())
	}
	// returned value is list (for combo ports - two values)
	mapstructure.Decode(resp["data"], &ports)
	// format ports summary
	res += fmtObj(ports, "ports.tmpl")
	for p := range ports {
		if ports[p].Link {
			linkUp = true
			break
		}
	}
	// get port bandwidth
	resp, err = apiGet(fmt.Sprintf("/sw/%s/ports/%s/bandwidth", ip, port))
	if err == nil {
		mapstructure.Decode(resp["data"], &bandwidth)
		res += fmtObj(bandwidth, "bandwidth.tmpl")
	}
	// get port counters
	resp, err = apiGet(fmt.Sprintf("/sw/%s/ports/%s/counters", ip, port))
	if err == nil {
		mapstructure.Decode(resp["data"], &counters)
		res += fmtObj(counters, "counters.tmpl")
	}
	if style == "full" {
		var v PortVlan
		var acl []PortACL
		var macTable []PortMac
		var arpTable []ARPEntry
		var arpTmp []ARPEntry
		var accessPorts []int
		var mcastMemberPorts []int
		var mcastSourcePorts []int
		// get vlan
		resp, err = apiGet(fmt.Sprintf("/sw/%s/ports/%s/vlan", ip, port))
		if err == nil {
			mapstructure.Decode(resp["data"], &v)
			res += fmtObj(v, "vlan.tmpl")
		}
		// acl, mac and arp only for access ports
		resp, err = apiGet(fmt.Sprintf("/sw/%s/ports/", ip))
		if err == nil {
			mapstructure.Decode(resp["data"].(map[string]interface{})["access_ports"], &accessPorts)
		}
		if !intInList(ports[0].Port, accessPorts) {
			goto END
		}
		// get acl
		resp, err = apiGet(fmt.Sprintf("/sw/%s/ports/%s/acl", ip, port))
		if err == nil {
			mapstructure.Decode(resp["data"], &acl)
			res += fmtObj(acl, "acl.tmpl")
		}
		// get multicast ports
		resp, err = apiGet(fmt.Sprintf("/sw/%s/multicast", ip))
		if err == nil {
			mapstructure.Decode(resp["data"].(map[string]interface{})["member"], &mcastMemberPorts)
			mapstructure.Decode(resp["data"].(map[string]interface{})["source"], &mcastSourcePorts)
			if len(mcastSourcePorts) == 0 {
				res += "\n<b>No multicast source ports</b>" + WARNCHAR + "\n"
			}
			res += "\n<i>Multicast: </i><code>"
			if intInList(ports[0].Port, mcastMemberPorts) {
				res += "enabled"
			} else {
				res += "disabled"
			}
			res += "</code>\n"
		}
		// get mac table only if link is up
		if !linkUp {
			goto END
		}
		resp, err = apiGet(fmt.Sprintf("/sw/%s/ports/%s/mac", ip, port))
		if err == nil {
			mapstructure.Decode(resp["data"], &macTable)
			res += fmtObj(macTable, "mac.tmpl")
		}
		// only if mac address table is not empty
		if len(macTable) == 0 {
			goto END
		}
		// get arp table for acl permit ip
		for _, a := range acl {
			if a.Mode == "permit" {
				resp, err = requestAPI("POST", "/arpsearch", map[string]interface{}{"ip": a.IP})
				if err == nil {
					mapstructure.Decode(resp["data"], &arpTmp)
					// append to global arp
					arpTable = append(arpTable, arpTmp...)
				} else {
					res += fmtErr("Failed to get arp by ip")
				}
			}
		}
		// get arp for each mac address
		for _, m := range macTable {
			resp, err = requestAPI("POST", "/arpsearch", map[string]interface{}{"mac": m.Mac, "src_sw_ip": ip})
			if err == nil {
				mapstructure.Decode(resp["data"], &arpTmp)
				// append to global arp
				arpTable = append(arpTable, arpTmp...)
			} else {
				res += fmtErr("Failed to get arp by mac")
			}
		}
		// remove duplicate entries from global arp table
		arpTmp = arpTable
		arpTable = nil
		for _, a := range arpTmp {
			dup := false
			for _, u := range arpTable {
				if u == a {
					dup = true
					break
				}
			}
			if !dup {
				arpTable = append(arpTable, a)
			}
		}

		res += fmtObj(arpTable, "arp.tmpl")

	END:
	}
	res += printUpdated()

	return res
}

// clear port counters
func portClear(ip string, port string) string {
	resp, err := apiDelete(fmt.Sprintf("/sw/%s/ports/%s/counters", ip, port))
	if err != nil {
		return fmtErr(err.Error())
	}
	if resp["detail"] == nil {
		logError(fmt.Sprintf("[clear %s %s] API returned no detail, raw data: %v", ip, port, resp))
		return fmtErr("Empty response")
	}
	return resp["detail"].(string)
}

// get ip summary
func ipCalc(ip string) string {
	var res string
	resp, err := apiGet(fmt.Sprintf("/ipcalc/%s/", ip))
	if err != nil {
		return fmtErr(err.Error())
	}
	var calc IPCalc
	mapstructure.Decode(resp["data"], &calc)
	res += fmtObj(calc, "ipcalc.tmpl")
	return res
}

// TELEGRAM COMMANDS HANDLERS

// new user handler
func newUserHandler(u *tgbotapi.User) {
	msg := fmt.Sprintf("User <a href=\"tg://user?id=%d\">%s</a> "+
		" requests authorization:\nid: <code>%d</code>", u.ID, u, u.ID)
	sendTo(CFG.Admin, msg)
	sendTo(u.ID, "Your request is accepted. Waiting confirmation from admin.")
}

// admin command handler
func adminHandler(msg string) string {
	cmd, arg := splitArgs(msg)
	var res string
	var err error
	switch cmd {
	case "list":
		for id, u := range CFG.Users {
			res += fmt.Sprintf(
				"<code>%d</code> - <a href=\"tg://user?id=%d\">%s</a>\n", id, id, u.Name)
		}
	case "add":
		res = manageUser(arg, true)
	case "del":
		res = manageUser(arg, false)
	case "send":
		user, text := splitArgs(arg)
		id, _ := strconv.ParseInt(user, 10, 64)
		err = sendTo(id, text)
		if err == nil {
			res = "Message sent"
		} else {
			res = fmt.Sprintf("Message not sent: %v", err)
		}
	case "broadcast":
		res = broadcastSend(arg)
	case "reload":
		err = loadConfig()
		if err != nil {
			res = "Failed"
		} else {
			res = "Config reloaded"
		}
	default:
		res = HELPADMIN
	}
	return res
}

// parse raw input handler
func rawHandler(raw string) (string, tgbotapi.InlineKeyboardMarkup) {
	var res string                       // text message result
	var kb tgbotapi.InlineKeyboardMarkup // inline keyboard markup
	cmd, args := splitArgs(raw)
	ip := fullIP(cmd, false)
	switch {
	// empty input
	case raw == "":
		res = "You are in raw command mode."
	// cmd is ip address
	case ip != "":
		// ip is sw ip
		if fullIP(ip, true) != "" {
			port, args := splitArgs(args)
			res, kb = swHandler(ip, port, args)
			// ip is client ip
		} else {
			res = ipHandler(ip, args)
		}
	default:
		res = fmtErr("Failed to parse raw input.")
		logError(fmt.Sprintf("[rawHandler] Failed to parse: %s", raw))
	}
	return res, kb
}

// switch ip handler
func swHandler(ip string, port string, args string) (string, tgbotapi.InlineKeyboardMarkup) {
	var res string                       // text message result
	var kb tgbotapi.InlineKeyboardMarkup // inline keyboard markup
	view := []string{"short", "full"}    // view styles for port summary
	idx := 0                             // default index - short view
	logDebug(fmt.Sprintf("[swHandler] ip: %s, port: %s, args: '%s'", ip, port, args))
	if port == "" {
		res = swSummary(ip, "full")
		goto RETURN
	}
	// clear counters if needed
	if strings.Contains(args, "clear") {
		logDebug(fmt.Sprintf("[swHandler] Clear result: %s", portClear(ip, port)))
	}
	// second part is for backward compatibility (arg f[ull] in raw command)
	if strings.Contains(args, "full") || args != "" && strings.HasPrefix("full", args) {
		idx = 1
	}
	//
	kb = genKeyboard([][]map[string]string{
		{
			// inverted view for full/short button calculated as (1 - idx)
			{view[1-idx]: fmt.Sprintf("raw edit %s %s %s", ip, port, view[1-idx])},
		},
		{
			{"refresh": fmt.Sprintf("raw edit %s %s %s", ip, port, view[idx])},
			{"clear": fmt.Sprintf("raw edit %s %s %s clear", ip, port, view[idx])},
			{"repeat": fmt.Sprintf("raw send %s %s %s", ip, port, view[idx])},
		},
	})
	// for ports switch view is always short
	res = swSummary(ip, "short")
	// get port summary only if no errors
	if !strings.Contains(res, "ERROR") {
		res += portSummary(ip, port, view[idx])
	}
RETURN:
	return res, kb
}

// client ip handler
func ipHandler(ip string, args string) string {
	// dummy calculator
	return ipCalc(ip)
}

// search mode handler
func searchHandler(kw string, page int) (string, tgbotapi.InlineKeyboardMarkup) {
	var res string                       // text message result
	var kb tgbotapi.InlineKeyboardMarkup // inline keyboard markup
	if kw == "" {
		res = "You are in search mode"
		return res, kb
	}
	resp, err := requestAPI("POST", "/db/search", map[string]interface{}{"keyword": kw, "page": page, "per_page": 4})
	if err != nil {
		res = fmtErr(err.Error())
	} else {
		var result DBSearch
		err = mapstructure.Decode(resp, &result)
		if err != nil {
			logError(fmt.Sprintf("[search] %v", err))
		} else {
			res = fmtObj(result, "search.tmpl")
			// callback pagination
			if result.Meta.Pages.Total > 1 {
				// make empty buttons row
				buttons := [][]map[string]string{{}}
				if page > 1 {
					if page > 2 {
						// first page
						buttons[0] = append(buttons[0], map[string]string{"<<": fmt.Sprintf("search edit %s %d", kw, 1)})
					}
					// previous page
					buttons[0] = append(buttons[0], map[string]string{"<": fmt.Sprintf("search edit %s %d", kw, page-1)})
				}
				if page < result.Meta.Pages.Total {
					// next page
					buttons[0] = append(buttons[0], map[string]string{">": fmt.Sprintf("search edit %s %d", kw, page+1)})
					if page < result.Meta.Pages.Total-1 {
						// last page
						buttons[0] = append(buttons[0], map[string]string{">>": fmt.Sprintf("search edit %s %d", kw, result.Meta.Pages.Total)})
					}
				}
				kb = genKeyboard(buttons)
			}
		}
	}
	return res, kb
}

// start user pinger
func pingerStart(uid int64, host string) error {
	// one user can ping one host at time
	if _, exist := Pingers[uid]; exist {
		pingerStop(uid)
	}
	logDebug(fmt.Sprintf("[ping] [%s] starting %s", CFG.Users[uid].Name, host))
	p, err := ping.NewPinger(host)
	if err != nil {
		logError(fmt.Sprintf("[ping] [%s] [%s] %v", CFG.Users[uid].Name, host, err))
		return err
	}
	// start message
	p.OnSetup = func() {
		// add static keyboard with stop button
		kb := tgbotapi.NewReplyKeyboard(tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("stop")))
		res := fmt.Sprintf("<pre>PING %s (%v) %d (%d) bytes of data.</pre>",
			p.Addr(), p.IPAddr(), p.Size, p.Size+28)
		sendMessage(uid, res, kb)
	}
	// send ping result for each packet
	p.OnRecv = func(pkt *ping.Packet) {
		sendTo(uid, fmt.Sprintf("<pre>%d bytes from %v: icmp_seq=%d time=%v</pre>",
			pkt.Nbytes, pkt.IPAddr, pkt.Seq, fmtRTT(pkt.Rtt)))
	}
	p.OnDuplicateRecv = func(pkt *ping.Packet) {
		sendTo(uid, fmt.Sprintf("<pre>%d bytes from %v: icmp_seq=%d time=%v (DUP!)</pre>",
			pkt.Nbytes, pkt.IPAddr, pkt.Seq, fmtRTT(pkt.Rtt)))
	}
	// send total statistics when stopped
	p.OnFinish = func(stats *ping.Statistics) {
		res := fmt.Sprintf("<pre>%s (%s) stats:\n"+
			"%d sent, %d received, %v%% loss\n"+
			"rtt min/avg/max/stddev:\n%v/%v/%v/%v</pre>",
			stats.Addr, stats.IPAddr,
			stats.PacketsSent, stats.PacketsRecv, stats.PacketLoss,
			fmtRTT(stats.MinRtt), fmtRTT(stats.AvgRtt), fmtRTT(stats.MaxRtt), fmtRTT(stats.StdDevRtt))
		// remove keyboard only if no new pinger is running
		if _, exist := Pingers[uid]; exist {
			sendTo(uid, res)
		} else {
			kb := tgbotapi.NewRemoveKeyboard(true)
			sendMessage(uid, res, kb)
		}
	}
	// add pinger to global list
	Pingers[uid] = *p
	// run ping in goroutine
	go p.Run()
	return err
}

// stop user pinger
func pingerStop(uid int64) {
	// check if pinger exists and stop it
	if _, exist := Pingers[uid]; exist {
		p := Pingers[uid]
		logDebug(fmt.Sprintf("[ping] [%s] stopping %s", CFG.Users[uid].Name, p.Addr()))
		p.Stop()
		// remove pinger from global list
		delete(Pingers, uid)
	}
}

// ping mode handler
func pingHandler(msg string, uid int64) string {
	var res string // text message result
	switch msg {
	case "":
		res = "You are in ping mode. Send <b>host</b> to start pinging. Send <code>stop</code> to stop pinging."
	case "stop":
		pingerStop(uid)
	default:
		if fullIP(msg, true) != "" {
			return fmtErr("Impossible to ping switch ip without violating network conception. Use /raw mode for availability checks.")
		} else if ip := fullIP(msg, false); ip != "" {
			msg = ip
		}
		err := pingerStart(uid, msg)
		if err != nil {
			res = fmtErr(err.Error())
		}
	}
	return res
}

// MAIN APP
func main() {
	loadConfig()
	// serve telegram updates
	for u := range initBot() {
		// empty updates if user blocked or restarted bot
		if u.FromChat() == nil {
			logWarning("Empty update")
			continue
		}
		uid := u.FromChat().ID
		// for unauthorized users only start cmd is available
		if !userIsAuthorized(uid) && uid != CFG.Admin {
			if u.Message != nil && u.Message.Command() == "start" {
				newUserHandler(u.SentFrom())
			}
			// skip any other updates from unauthorized users
			continue
		}
		// message updates
		if u.Message != nil {
			logInfo(fmt.Sprintf("[message] [%s] %s", CFG.Users[uid].Name, u.Message.Text))
			var msg string                       // input message
			var res string                       // output message
			var kb tgbotapi.InlineKeyboardMarkup // output keyboard markup
			cmd := u.Message.Command()
			cmdArgs := u.Message.CommandArguments()
			if cmd != "" {
				msg = cmdArgs
			} else {
				msg = u.Message.Text
			}

			// cmd processing
			switch cmd {
			case "help":
				res = HELPUSER
				goto SEND
			case "start":
				res = HELPUSER
				goto SEND
			case "admin":
				if uid == CFG.Admin {
					CFG.Users[uid].Mode = "admin"
				} else {
					res = fmtErr("You have no permissions to work in this mode.")
					goto SEND
				}
			case "raw":
				CFG.Users[uid].Mode = "raw"
			case "report":
				CFG.Users[uid].Mode = "report"
			case "search":
				CFG.Users[uid].Mode = "search"
			case "ping":
				CFG.Users[uid].Mode = "ping"
			// no command
			case "":
				// skip
			// wrong command
			default:
				res = fmtErr("Unknown command.")
				goto SEND
			}

			// msg processing
			switch CFG.Users[uid].Mode {
			case "admin":
				res = adminHandler(msg)
			case "report":
				if msg == "" {
					res = "You are in report mode. " +
						"Send message with your report, you can also attach screenshots or other media.\n" +
						"To cancel and return to raw command mode, send /raw."
					goto SEND
				}
				fwd := tgbotapi.NewForward(CFG.ReportChannel, uid, u.Message.MessageID)
				_, err := Bot.Send(fwd)
				if err != nil {
					logError(fmt.Sprintf("[report] %v", err))
					res += "Your report failed. Contact admin to check logs. "
				} else {
					res += "Your report has been sent. "
				}
				res += "Send another message or return to raw command mode by sending /raw."
			case "search":
				// search and go to the first page
				res, kb = searchHandler(msg, 1)
			case "ping":
				// res, kb = pingHandler(msg)
				res = pingHandler(msg, uid)
			default: // default is raw mode
				res, kb = rawHandler(msg)
			}
		SEND:
			if res != "" {
				if len(kb.InlineKeyboard) > 0 {
					sendMessage(uid, res, kb)
				} else {
					sendTo(uid, res)
				}
			}
			// callback updates
		} else if u.CallbackData() != "" {
			logInfo(fmt.Sprintf("[callback] [%s] %s", CFG.Users[uid].Name, u.CallbackData()))
			msg := u.CallbackQuery.Message
			var res string                       // output message
			var kb tgbotapi.InlineKeyboardMarkup // output keyboard markup
			mode, args := splitArgs(u.CallbackData())
			action, rawCmd := splitArgs(args)

			switch mode {
			case "raw":
				res, kb = rawHandler(rawCmd)
			case "search":
				// cut last argument - page number and convert to int
				kw, p := splitLast(rawCmd)
				page, _ := strconv.Atoi(p)
				res, kb = searchHandler(kw, page)
			default:
				logWarning(fmt.Sprintf("[callback] wrong mode: %s", mode))
				goto CALLBACK
			}
			// restore mode if it was changed by another command
			CFG.Users[uid].Mode = mode

			switch action {
			case "send":
				if len(kb.InlineKeyboard) > 0 {
					sendMessage(uid, res, kb)
				} else {
					sendTo(uid, res)
				}
			case "edit":
				if len(kb.InlineKeyboard) > 0 {
					editTextAndKeyboard(msg, res, kb)
				} else {
					editText(msg, res)
				}
			default:
				logWarning(fmt.Sprintf("[callback] wrong action: %s", action))
			}
		CALLBACK:
			Bot.Request(tgbotapi.NewCallback(u.CallbackQuery.ID, "Done"))
		}
	}
}

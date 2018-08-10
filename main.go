package main

import (
	"context"
	"fmt"
	"github.com/mafredri/cdp"
	"github.com/mafredri/cdp/devtool"
	"github.com/mafredri/cdp/protocol/debugger"
	"github.com/mafredri/cdp/protocol/dom"
	"github.com/mafredri/cdp/protocol/page"
	"github.com/mafredri/cdp/rpcc"
	chrome "github.com/mkenney/go-chrome/tot"
	"io/ioutil"
	"regexp"
	"sync"
	"time"
)

//func init() {
//	level, _ := log.ParseLevel("debug")
//	log.SetLevel(level)
//	//	log.SetFormatter(&logfmt.TextFormat{})
//}

func main() {
	browser := chrome.New(
		&chrome.Flags{
			"disable-gpu":              nil,
			"headless":                 nil,
			"remote-debugging-address": "127.0.0.1",
			"remote-debugging-port":    9222,
			"no-first-run":             nil,
			"no-sandbox":               nil,
		},
		"/usr/bin/google-chrome",
		//"/usr/bin/headless_shell",
		"/home1/irteamsu/tmp",
		"/dev/null",
		"/dev/null",
	)

	if err := browser.Launch(); err != nil {
		panic(err.Error())
	}
	defer browser.Close()

	run(10*time.Second, "https://www.w3schools.com/jsref/prop_loc_href.asp")

}

func run(timeout time.Duration, url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Use the DevTools HTTP/JSON API to manage targets (e.g. pages, webworkers).
	devt := devtool.New("http://127.0.0.1:9222")
	pt, err := devt.Get(ctx, devtool.Page)
	if err != nil {
		pt, err = devt.Create(ctx)
		if err != nil {
			return err
		}
	}

	// Initiate a new RPC connection to the Chrome Debugging Protocol target.
	conn, err := rpcc.DialContext(ctx, pt.WebSocketDebuggerURL)
	if err != nil {
		return err
	}
	defer conn.Close() // Leaving connections open will leak memory.

	c := cdp.NewClient(conn)

	// Open a DOMContentEventFired client to buffer this event.
	domContent, err := c.Page.DOMContentEventFired(ctx)
	if err != nil {
		return err
	}
	defer domContent.Close()

	ScriptParseClient, err := c.Debugger.ScriptParsed(ctx)
	if err != nil {
		return err
	}
	defer ScriptParseClient.Close()

	// Enable events on the Page domain, it's often preferrable to create
	// event clients before enabling events so that we don't miss any.
	if err = c.Page.Enable(ctx); err != nil {
		return err
	}

	if _, err = c.Debugger.Enable(ctx); err != nil {
		return err
	}

	// Create the Navigate arguments with the optional Referrer field set.
	navArgs := page.NewNavigateArgs(url).
		SetReferrer("https://duckduckgo.com")
	nav, err := c.Page.Navigate(ctx, navArgs)
	if err != nil {
		return err
	}

	// Wait until we have a DOMContentEventFired event.
	if _, err = domContent.Recv(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	var getLinkCode = regexp.MustCompile(`(.*)?(location\.href)((\((.*)\))|(.*\=(.*)))(.*)?`)
	go func() {
		for {
			reply, err := ScriptParseClient.Recv()
			if err != nil {
				return
			}
			fmt.Printf("[ID] : %s , start : %d , end %d \n", reply.ScriptID, reply.StartLine, reply.EndLine)
			wg.Add(1)
			go func(reply *debugger.ScriptParsedReply) {
				source, err := c.Debugger.GetScriptSource(ctx, &debugger.GetScriptSourceArgs{reply.ScriptID})
				if err != nil {
					return
				}
				matches := getLinkCode.FindAllStringSubmatch(source.ScriptSource, -1)
				for _, v := range matches {
					for i, s := range v {
						fmt.Println(i, ", ", s)
					}
				}
				wg.Done()
			}(reply)
		}
	}()

	fmt.Printf("Page loaded with frame ID: %s\n", nav.FrameID)

	// Fetch the document root node. We can pass nil here
	// since this method only takes optional arguments.
	doc, err := c.DOM.GetDocument(ctx, nil)
	if err != nil {
		return err
	}

	// Get the outer HTML for the page.
	_, err = c.DOM.GetOuterHTML(ctx, &dom.GetOuterHTMLArgs{
		NodeID: &doc.Root.NodeID,
	})
	if err != nil {
		return err
	}

	//fmt.Printf("HTML: %s\n", result.OuterHTML)

	// Capture a screenshot of the current page.
	screenshotName := "screenshot.jpg"
	screenshotArgs := page.NewCaptureScreenshotArgs().
		SetFormat("jpeg").
		SetQuality(80)
	screenshot, err := c.Page.CaptureScreenshot(ctx, screenshotArgs)
	if err != nil {
		return err
	}
	if err = ioutil.WriteFile(screenshotName, screenshot.Data, 0644); err != nil {
		return err
	}

	fmt.Printf("Saved screenshot: %s\n", screenshotName)
	wg.Wait()

	return nil
}

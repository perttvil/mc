/*
 * MinIO Client (C) 2019 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/minio/cli"
	json "github.com/minio/mc/pkg/colorjson"
	"github.com/minio/mc/pkg/console"
	"github.com/minio/mc/pkg/probe"
	"github.com/minio/minio/pkg/madmin"
)

const logTimeFormat string = "15:04:05 MST 01/02/2006"

var adminConsoleFlags = []cli.Flag{
	cli.IntFlag{
		Name:  "limit, l",
		Usage: "show last n log entries",
		Value: 10,
	},
}

var adminConsoleCmd = cli.Command{
	Name:            "console",
	Usage:           "show console logs for MinIO server",
	Action:          mainAdminConsole,
	Before:          setGlobalsFromContext,
	Flags:           append(adminConsoleFlags, globalFlags...),
	HideHelpCommand: true,
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} [FLAGS] TARGET [NODENAME]

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Show console logs for a MinIO server with alias 'play'
     $ {{.HelpName}} play

  2. Show last 5 log entries for node 'node1' on MinIO server with alias 'cluster1'
     $ {{.HelpName}} --limit 5 cluster1 node1
`,
}

func checkAdminLogSyntax(ctx *cli.Context) {
	if len(ctx.Args()) == 0 || len(ctx.Args()) > 2 {
		cli.ShowCommandHelpAndExit(ctx, "console", 1) // last argument is exit code
	}
}

// Extend madmin.LogInfo to add String() and JSON() methods
type logMessage struct {
	madmin.LogInfo
}

// JSON - jsonify loginfo
func (l logMessage) JSON() string {
	logJSON, err := json.MarshalIndent(&l, "", " ")
	fatalIf(probe.NewError(err), "Unable to marshal into JSON.")

	return string(logJSON)

}
func getLogTime(lt string) string {
	tm, err := time.Parse(time.RFC3339Nano, lt)
	if err != nil {
		return lt
	}
	return tm.Format(logTimeFormat)
}

// String - return colorized loginfo as string.
func (l logMessage) String() string {
	var hostStr string
	var b = &strings.Builder{}

	if l.NodeName != "" {
		hostStr = fmt.Sprintf("%s ", colorizedNodeName(l.NodeName))
	}
	log := l.LogInfo
	if log.ConsoleMsg != "" {
		if strings.HasPrefix(log.ConsoleMsg, "\n") {
			fmt.Fprintf(b, "%s\n", hostStr)
			log.ConsoleMsg = strings.TrimPrefix(log.ConsoleMsg, "\n")
		}
		fmt.Fprintf(b, "%s %s", hostStr, log.ConsoleMsg)
		return b.String()
	}
	traceLength := len(l.Trace.Source)

	apiString := "API: " + l.API.Name + "("
	if l.API.Args != nil && l.API.Args.Bucket != "" {
		apiString = apiString + "bucket=" + l.API.Args.Bucket
	}
	if l.API.Args != nil && l.API.Args.Object != "" {
		apiString = apiString + ", object=" + l.API.Args.Object
	}
	apiString += ")"

	var msg = console.Colorize("LogMessage", l.Trace.Message)

	fmt.Fprintf(b, "\n%s %s", hostStr, console.Colorize("Api", apiString))
	fmt.Fprintf(b, "\n%s Time: %s", hostStr, getLogTime(l.Time))
	fmt.Fprintf(b, "\n%s DeploymentID: %s", hostStr, l.DeploymentID)
	if l.RequestID != "" {
		fmt.Fprintf(b, "\n%s RequestID: %s", hostStr, l.RequestID)
	}
	if l.RemoteHost != "" {
		fmt.Fprintf(b, "\n%s RemoteHost: %s", hostStr, l.RemoteHost)
	}
	if l.UserAgent != "" {
		fmt.Fprintf(b, "\n%s UserAgent: %s", hostStr, l.UserAgent)
	}
	fmt.Fprintf(b, "\n%s Error: %s", hostStr, msg)

	for key, value := range l.Trace.Variables {
		if value != "" {
			fmt.Fprintf(b, "\n%s %s=%s", hostStr, key, value)
		}
	}
	for i, element := range l.Trace.Source {
		fmt.Fprintf(b, "\n%s %8v: %s", hostStr, traceLength-i, element)

	}
	logMsg := strings.TrimPrefix(b.String(), "\n")
	return fmt.Sprintf("%s\n", logMsg)
}

// mainAdminConsole - the entry function of console command
func mainAdminConsole(ctx *cli.Context) error {
	// Check for command syntax
	checkAdminLogSyntax(ctx)
	console.SetColor("LogMessage", color.New(color.Bold, color.FgRed))
	console.SetColor("Api", color.New(color.Bold, color.FgWhite))
	for _, c := range colors {
		console.SetColor(fmt.Sprintf("Node%d", c), color.New(c))
	}
	aliasedURL := ctx.Args().Get(0)
	var node string
	if len(ctx.Args()) > 1 {
		node = ctx.Args().Get(1)
	}
	var limit int
	if ctx.IsSet("limit") {
		limit = ctx.Int("limit")
		if limit <= 0 {
			fatalIf(errInvalidArgument().Trace(ctx.Args()...), "please set a proper limit, for example: '--limit 5' to display last 5 logs, omit this flag to display all available logs")
		}
	}
	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	if err != nil {
		fatalIf(err.Trace(aliasedURL), "Cannot initialize admin client.")
		return nil
	}
	doneCh := make(chan struct{})
	defer close(doneCh)

	// Start listening on all console log activity.
	logCh := client.GetLogs(node, limit, doneCh)
	for logInfo := range logCh {
		if logInfo.Err != nil {
			fatalIf(probe.NewError(logInfo.Err), "Cannot listen to console logs")
		}
		// drop nodeName from output if specified as cli arg
		if node != "" {
			logInfo.NodeName = ""
		}
		printMsg(logMessage{logInfo})
	}
	return nil
}

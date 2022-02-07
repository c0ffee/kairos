package main

import (
	//"fmt"

	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mudler/c3os/installer/systemd"
	edgeVPNClient "github.com/mudler/edgevpn/api/client"
	service "github.com/mudler/edgevpn/api/client/service"
	nodepair "github.com/mudler/go-nodepair"
	qr "github.com/mudler/go-nodepair/qrcode"
	"github.com/pterm/pterm"
	"github.com/urfave/cli"
)

func main() {
	app := &cli.App{
		Name:        "c3os",
		Version:     "0.1",
		Author:      "Ettore Di Giacinto",
		Usage:       "c3os (register|install)",
		Description: "c3os registers and installs c3os boxes",
		UsageText:   ``,
		Copyright:   "Ettore Di Giacinto",

		Commands: []cli.Command{
			{
				Name: "register",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name: "config",
					},
					&cli.StringFlag{
						Name: "device",
					},
					&cli.BoolFlag{
						Name: "reboot",
					},
				},
				Action: func(c *cli.Context) error {
					args := c.Args()
					var ref string
					if len(args) == 1 {
						ref = args[0]
					}

					b, _ := ioutil.ReadFile(c.String("config"))
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					// dmesg -D to suppress tty ev

					fmt.Println("Sending registration payload, please wait")

					config := map[string]string{
						"device": c.String("device"),
						"cc":     string(b),
					}

					if c.Bool("reboot") {
						config["reboot"] = ""
					}

					err := nodepair.Send(
						ctx,
						config,
						nodepair.WithReader(qr.Reader),
						nodepair.WithToken(ref),
					)
					if err != nil {
						return err
					}

					fmt.Println("Payload sent, installation will start on the machine briefly")

					return nil
				},
			},
			{
				Name:      "setup",
				Aliases:   []string{"s"},
				UsageText: "Automatically setups the node",
				Action: func(c *cli.Context) error {
					dir := "/oem"
					args := c.Args()
					if len(args) > 0 {
						dir = args[0]
					}

					return setup(dir)
				},
			},
			{
				Name: "get-kubeconfig",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name: "api",
					},
				},
				Action: func(c *cli.Context) error {
					cc := service.NewClient(
						"c3os",
						edgeVPNClient.NewClient(edgeVPNClient.WithHost(fmt.Sprintf("http://%s", c.String("api")))))
					str, _ := cc.Get("kubeconfig", "master")
					b, _ := base64.URLEncoding.DecodeString(str)
					masterIP, _ := cc.Get("master", "ip")
					fmt.Println(strings.ReplaceAll(string(b), "127.0.0.1", masterIP))
					return nil
				},
			},
			{
				Name:    "install",
				Aliases: []string{"i"},
				Action: func(c *cli.Context) error {

					printBanner(banner)
					tk := nodepair.GenerateToken()

					pterm.DefaultBox.WithTitle("Installation").WithTitleBottomRight().WithRightPadding(0).WithBottomPadding(0).Println(
						`Welcome to c3os!
p2p device installation enrollment is starting.
A QR code will be displayed below. 
In another machine, run "c3os register" with the QR code visible on screen,
or "c3os register <file>" to register the machine from a photo.
IF the qrcode is not displaying correctly,
try booting with another vga option from the boot cmdline (e.g. vga=791).`)

					pterm.Info.Println("Starting in 5 seconds...")
					pterm.Print("\n\n") // Add two new lines as spacer.

					time.Sleep(5 * time.Second)

					qr.Print(tk)

					r := map[string]string{}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()

					go func() {
						prompt("")
						// give tty1 back
						systemd.StartUnit("getty@tty1")
						cancel()
					}()

					noSpinner := os.Getenv("NOSPINNER") == "true"
					var spinnerSuccess *pterm.SpinnerPrinter
					if !noSpinner {
						spinnerSuccess, _ = pterm.DefaultSpinner.Start("p2p device enrollment started, press any key to abort pairing and drop to shell. To re-start enrollment, run 'c3os install'. Waiting for registration. ")
					}
					if err := nodepair.Receive(ctx, &r, nodepair.WithToken(tk)); err != nil {
						if !noSpinner {
							spinnerSuccess.Stop()
						}
						return err
					}
					if !noSpinner {
						spinnerSuccess.Stop()
					}

					if len(r) == 0 {
						return errors.New("no configuration, stopping installation")
					}

					pterm.Info.Println("Starting installation")
					runInstall(r)

					prompt("Installation completed, press any key to reboot")

					exec.Command("reboot").Run()
					return nil
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

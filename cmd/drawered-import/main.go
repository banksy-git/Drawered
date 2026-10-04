// Command drawered-import copies an InvenTree inventory into a Drawered data
// directory. See SPEC.md section 18.
package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "os"
    "os/signal"
    "strings"

    "drawered/internal/app"
    "drawered/internal/importer"
    "drawered/internal/inventree"
)

const usage = `usage: drawered-import --url <inventree base URL> [options]

Copies stock locations, part categories, parts (one supplier each), stock
levels and part images from InvenTree into Drawered.

InvenTree credentials come from the environment:
  INVENTREE_TOKEN                          an API token, or
  INVENTREE_USERNAME, INVENTREE_PASSWORD   to fetch one

Drawered is located with the usual DRAWERED_* variables, chiefly
DRAWERED_DATA_DIR. Back up the data directory before importing.

Options:
`

func main() {
    var (
        baseURL      = flag.String("url", "", "InvenTree base URL, e.g. https://inventree.example.com")
        dryRun       = flag.Bool("dry-run", false, "fetch and report what would be imported; write nothing")
        skipInactive = flag.Bool("skip-inactive", false, "do not import inactive parts")
        noStock      = flag.Bool("no-stock", false, "do not import stock levels")
        noImages     = flag.Bool("no-images", false, "do not download part images")
        currency     = flag.String("default-currency", "", "currency for prices without one (default DRAWERED_DEFAULT_CURRENCY)")
        jsonReport   = flag.Bool("json", false, "print the final report as JSON")
    )
    flag.Usage = func() {
        fmt.Fprint(os.Stderr, usage)
        flag.PrintDefaults()
    }
    flag.Parse()
    if *baseURL == "" || flag.NArg() > 0 {
        flag.Usage()
        os.Exit(2)
    }
    if err := run(*baseURL, importer.Options{
        DryRun: *dryRun, SkipInactive: *skipInactive, NoStock: *noStock, NoImages: *noImages,
        DefaultCurrency: strings.ToUpper(*currency),
        Log:             func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) },
    }, *jsonReport); err != nil {
        fmt.Fprintln(os.Stderr, "drawered-import:", err)
        os.Exit(1)
    }
}

func run(baseURL string, opt importer.Options, jsonReport bool) error {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()

    client := inventree.New(baseURL)
    if tok := os.Getenv("INVENTREE_TOKEN"); tok != "" {
        client.Token = tok
    } else if user := os.Getenv("INVENTREE_USERNAME"); user != "" {
        if err := client.Login(ctx, user, os.Getenv("INVENTREE_PASSWORD")); err != nil {
            return fmt.Errorf("login: %w", err)
        }
    } else {
        return fmt.Errorf("set INVENTREE_TOKEN, or INVENTREE_USERNAME and INVENTREE_PASSWORD")
    }

    cfg, svc, closeFn, err := app.Open(ctx)
    if err != nil {
        return err
    }
    defer closeFn()
    if opt.DryRun {
        fmt.Fprintf(os.Stderr, "Dry run against %s: nothing will be written.\n", cfg.DataDir)
    } else {
        fmt.Fprintf(os.Stderr, "Importing into %s. Make sure you have a backup.\n", cfg.DataDir)
    }

    rep, err := importer.Run(ctx, svc, client, opt)
    if rep != nil {
        if jsonReport {
            enc := json.NewEncoder(os.Stdout)
            enc.SetIndent("", "  ")
            enc.Encode(rep)
        } else {
            printReport(rep)
        }
    }
    return err
}

func printReport(r *importer.Report) {
    verb := "Imported"
    if r.DryRun {
        verb = "Would import"
    }
    row := func(name string, c importer.Counts) {
        fmt.Printf("  %-11s %5d created  %5d reused  %5d already imported  %5d failed\n",
            name, c.Created, c.Reused, c.Skipped, c.Failed)
    }
    fmt.Printf("%s:\n", verb)
    row("Locations", r.Locations)
    row("Categories", r.Categories)
    row("Parts", r.Parts)
    fmt.Printf("  %d stock entries, %d images, %d links\n", r.StockEntries, r.Images, r.Links)
    if r.Inactive > 0 {
        fmt.Printf("  %d inactive parts skipped\n", r.Inactive)
    }
    if len(r.Warnings) > 0 {
        fmt.Printf("\n%d warnings:\n", len(r.Warnings))
        for _, w := range r.Warnings {
            fmt.Println("  - " + w)
        }
    }
}

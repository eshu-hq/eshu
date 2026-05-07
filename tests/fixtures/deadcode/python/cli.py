import click
import typer

cli = click.Group()
app = typer.Typer()


@cli.command("sync")
def click_sync() -> str:
    return "ok"


@app.command()
def typer_serve() -> str:
    return "ok"


@app.callback()
def typer_root() -> None:
    return None

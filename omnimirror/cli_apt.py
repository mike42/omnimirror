import click
import shutil


@click.group()
def main():
    pass


@main.command(help='Download Debian APT package repository.')
@click.option('--output', metavar='DIR', required=True,
              help="Directory to save this repository to. This will be created if it does not yet exist.")
@click.option('--url', metavar='URL', required=True,
              help="Base URL to the repository, as it would appear in /etc/apt/sources.list. Only HTTP URLs are currently supported.")
@click.option('--suite', multiple=True, metavar='SUITE', required=True,
              help="The suite or release code-name to download, as it would appear immediately after the URL in /etc/apt/sources.list - often stable/testing or the code-name for a release. This may be specified multiple times, in which case the releases are downloaded in the order specified.")
@click.option('--component', multiple=True, metavar='COMPONENT',
              help="Components to consider for download, such as main, contrib non-free, universe, multiverse, etc. If this is not specified, then all components are downloaded. This may also be specified multiple times, in which case the requested components are downloaded in the order specified.")
@click.option('--arch', multiple=True, metavar='ARCH',
              help="Architectures to consider for download, such as amd64, arm64, etc. If this is not specified, then all architectures are downloaded. This may also be specified multiple times, in which case the requested architectures will be downloaded in the order specified. The 'all' architecture, for packages which are not architecture-specific, is implicitly always included.")
@click.option('--dry-run', is_flag=True,
              help="Fetch metadata but do not download any packages. This option can be used to discover which architectures and components are available in the specified repository.")
def download(output, url, suite, component, arch, dry_run):
    click.echo(f"output = {output}")
    click.echo(f"url = {url}")
    click.echo(f"suite = {suite}")
    click.echo(f"component = {component}")
    click.echo(f"arch = {arch}")
    click.echo(f"dry_run = {dry_run}")
    # Not done yet.
    click.echo("Not implemented", err=True)
    exit(1)

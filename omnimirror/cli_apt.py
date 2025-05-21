import lzma
import subprocess

import click
import os
import urllib.parse

from pathlib import Path

from debian.deb822 import Release, Packages


@click.group()
def main():
    pass

def join_url(url_base: str, filename: str) -> str:
    return urllib.parse.urljoin(url_base, filename.lstrip("/"))

def join_file_path(path_base: Path, filename: str) -> Path:
    """
    Join output dir + filename. Filenames may be provided by server, so we do a quick check that the joined filename
    is under the base path to prevent path traversal outside of that.
    """
    joined_path = path_base / filename.lstrip("/")
    if path_base not in joined_path.parents:
        raise ValueError(f"Resolved path '{joined_path}' is not under '{path_base}'. Refusing to write files there'.")
    return joined_path

def download_file(url_base: str, staging_output_base: Path, final_output_base: Path, filename: str, checksum=None, size=None):
    full_url = join_url(url_base, filename)
    staging_output_path = join_file_path(staging_output_base, filename)
    final_output_path = join_file_path(final_output_base, filename)
    if final_output_path.exists() and final_output_path.stat().st_size == size:
        # Some debugging. We might skip downloads if file exists w/ correct size
        click.echo("Size match")
    os.makedirs(staging_output_path.parent, exist_ok=True)
    subprocess.run(["curl", "--silent", "--fail", "--location", "-o", str(staging_output_path), str(full_url)], check=True)
    # TODO validate checksum before moving into place if known
    os.makedirs(final_output_path.parent, exist_ok=True)
    os.rename(staging_output_path, final_output_path)
    return staging_output_path, final_output_path

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
    # For debugging..
    click.echo(f"output = {output}")
    click.echo(f"url = {url}")
    click.echo(f"suite = {suite}")
    click.echo(f"component = {component}")
    click.echo(f"arch = {arch}")
    click.echo(f"dry_run = {dry_run}")

    output_path = Path(output)
    os.makedirs(output_path, exist_ok=True)
    staging_path = output_path / ".staging"
    os.makedirs(staging_path, exist_ok=True)

    for suite_name in suite:
        for metadata_filename in ["Release.gpg", "InRelease"]:
            download_file(url, staging_path, output_path, f"/dists/{suite_name}/{metadata_filename}")
        _, dest_path = download_file(url, staging_path, output_path, f"/dists/{suite_name}/Release")
        with open(dest_path, "r") as f:
            release = Release(f)
        # Using this as an example file
        test_file = next(x for x in release['sha256'] if x['name'] == 'main/binary-amd64/Packages.xz') # Note this will be zipped sometimes..
        _, dest_path_2 = download_file(url, staging_path, output_path, f"/dists/{suite_name}/{test_file['name']}",
                                       checksum=test_file['sha256'],
                                       size=int(test_file['size']))
        with lzma.open(dest_path_2, mode="rt", encoding="utf-8") as f:
            for package in Packages.iter_paragraphs(f):
                print(package["Package"], package.get("SHA256"), package["Filename"])

    click.echo("This is just a test. The repo is not actually usable.", err=True)
    exit(1)

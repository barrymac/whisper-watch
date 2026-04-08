import os
import time
import logging
import configparser
import requests
from watchdog.observers import Observer
from watchdog.events import FileSystemEventHandler

logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')


def create_dirs(config):
    os.makedirs(config['Local']['WatchDirectory'], exist_ok=True)
    os.makedirs(config['Local']['ProcessedDirectory'], exist_ok=True)
    os.makedirs(config['Local']['DownloadDirectory'], exist_ok=True)


def process_file(filepath, config):
    filename = os.path.basename(filepath)
    logging.info(f"Processing new file: {filename}")

    api_url = config['Service']['URL'].rstrip('/') + '/v1/translate'

    try:
        with open(filepath, 'rb') as f:
            resp = requests.post(
                api_url,
                files={'file': (filename, f)},
                timeout=600,
            )

        if resp.status_code != 200:
            logging.error(f"Translation API returned {resp.status_code}: {resp.text}")
            return

        result = resp.json()
        translation_text = result.get('text', '')

        logging.info("===========================================================")
        logging.info("==================== TRANSLATION START ====================")
        logging.info(f"File: {filename}")
        logging.info(f"Content: {translation_text}")
        logging.info("====================  TRANSLATION END  ====================")
        logging.info("===========================================================")

        base_name, _ = os.path.splitext(filename)
        download_dir = config['Local']['DownloadDirectory']
        txt_path = os.path.join(download_dir, f"{base_name}.txt")
        with open(txt_path, 'w', encoding='utf-8') as f:
            f.write(translation_text)
        logging.info(f"Saved translation to {txt_path}")

        processed_dir = config['Local']['ProcessedDirectory']
        os.rename(filepath, os.path.join(processed_dir, filename))
        logging.info(f"Moved {filename} to {processed_dir}")

    except requests.exceptions.Timeout:
        logging.error(f"Translation request timed out for {filename}")
    except requests.exceptions.ConnectionError as e:
        logging.error(f"Cannot connect to translation service: {e}")
    except Exception as e:
        logging.error(f"Unexpected error processing {filename}: {e}", exc_info=True)


class FileCreationHandler(FileSystemEventHandler):
    def __init__(self, config, processed_files, allowed_extensions):
        self.config = config
        self.processed_files = processed_files
        self.allowed_extensions = allowed_extensions

    def on_created(self, event):
        if event.is_directory:
            return

        filepath = event.src_path
        filename = os.path.basename(filepath)

        if (filename not in self.processed_files and
                filename.lower().endswith(self.allowed_extensions)):
            try:
                time.sleep(1)
                initial_size = os.path.getsize(filepath)
                time.sleep(1)
                if initial_size == os.path.getsize(filepath):
                    process_file(filepath, self.config)
                    self.processed_files.add(filename)
            except FileNotFoundError:
                logging.warning(f"File {filename} disappeared before processing.")
            except Exception as e:
                logging.error(f"Error handling new file {filename}: {e}", exc_info=True)


def main():
    config = configparser.ConfigParser()
    config.read('config.ini')
    if not config.sections():
        logging.error("Could not read config.ini or it is empty.")
        return

    if 'Service' not in config:
        logging.error("Missing [Service] section in config.ini.")
        return

    create_dirs(config)

    local_config = config['Local']
    watch_dir = local_config['WatchDirectory']
    processed_dir = local_config['ProcessedDirectory']
    allowed_extensions_str = local_config.get('AllowedExtensions', 'ogg,mp3')
    allowed_extensions = tuple(f".{ext.strip().lower()}" for ext in allowed_extensions_str.split(','))

    logging.info(f"Watching directory: {watch_dir} for files with extensions: {', '.join(allowed_extensions)}")
    logging.info(f"Translation service: {config['Service']['URL']}")

    processed_files = set(os.listdir(processed_dir))

    logging.info("Performing initial scan of watch directory...")
    for filename in os.listdir(watch_dir):
        filepath = os.path.join(watch_dir, filename)
        if (os.path.isfile(filepath) and
                filename not in processed_files and
                filename.lower().endswith(allowed_extensions)):
            try:
                time.sleep(0.5)
                if os.path.getsize(filepath) > 0:
                    process_file(filepath, config)
                    processed_files.add(filename)
            except Exception as e:
                logging.error(f"Error processing pre-existing file {filename}: {e}", exc_info=True)

    logging.info("Initial scan complete. Starting real-time watcher...")
    event_handler = FileCreationHandler(config, processed_files, allowed_extensions)
    observer = Observer()
    observer.schedule(event_handler, watch_dir, recursive=False)
    observer.start()

    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        observer.stop()
        logging.info("Observer stopped by user.")
    observer.join()
    logging.info("Watcher has shut down.")


if __name__ == "__main__":
    main()

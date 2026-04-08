import subprocess
import sys
import os
import configparser

def main():
    if len(sys.argv) < 3:
        print("Usage: python remote_whisper_translate.py <audio_file_path> <output_dir>", file=sys.stderr)
        sys.exit(1)

    audio_file_path = sys.argv[1]
    output_dir = sys.argv[2]

    # Ensure output directory exists
    try:
        os.makedirs(output_dir, exist_ok=True)
    except OSError as e:
        print(f"Error: Could not create output directory at {output_dir}: {e}", file=sys.stderr)
        sys.exit(1)

    # Load configuration for Whisper parameters
    script_dir = os.path.dirname(__file__)
    config_path = os.path.join(script_dir, 'config.ini')
    config = configparser.ConfigParser()
    config.read(config_path)

    if 'Whisper' not in config:
        print("Error: [Whisper] section not found in config.ini", file=sys.stderr)
        sys.exit(1)
    
    whisper_config = config['Whisper']
    model = whisper_config.get('Model', 'large-v3')
    language = whisper_config.get('Language', 'pt')
    conda_env_name = whisper_config.get('CondaEnvName')
    conda_path = whisper_config.get('CondaPath') # Can be None

    # Define the base command prefix, using conda if an environment is specified
    base_command = []
    executable_for_error_msg = 'whisper'
    if conda_env_name:
        conda_executable = conda_path if conda_path else 'conda'
        executable_for_error_msg = conda_executable
        base_command = [conda_executable, 'run', '-n', conda_env_name]
        print(f"Attempting to run whisper within Conda environment: {conda_env_name} using '{conda_executable}'")

    # --- 1. Translation Command (generates .txt) ---
    translate_command = base_command + [
        'whisper', audio_file_path,
        '--task', 'translate',
        '--model', model,
        '--language', language,
        '--output_format', 'txt',
        '--output_dir', output_dir
    ]

    try:
        # Execute translation
        print(f"Running translation: {' '.join(translate_command)}")
        subprocess.run(translate_command, check=True, capture_output=True, text=True)
        print("Translation task completed.")

    except subprocess.CalledProcessError as e:
        print(f"Error executing Whisper: {e}", file=sys.stderr)
        print(f"Whisper STDOUT: {e.stdout}", file=sys.stderr)
        print(f"Whisper STDERR: {e.stderr}", file=sys.stderr)
        sys.exit(1)
    except FileNotFoundError as e:
        print(f"Error: Command not found: '{executable_for_error_msg}'.", file=sys.stderr)
        if conda_env_name:
            print("Please ensure the CondaPath in config.ini is correct or that 'conda' is in the remote system's PATH for non-interactive sessions.", file=sys.stderr)
        else:
            print("Please ensure the 'whisper' executable is in the remote system's PATH.", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()

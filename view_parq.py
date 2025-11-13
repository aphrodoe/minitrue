#!/usr/bin/env python3
"""
Script to view data from custom .parq files created by the minitrue storage engine.
This implements the Gorilla compression decompression algorithm.
"""

import struct
import sys
from datetime import datetime
from typing import List, Tuple

# Constants from storage_engine.go
MAGIC_NUMBER = 0x50415251
HEADER_SIZE = 32


class BitReader:
    """Bit-level reader for decompressing Gorilla-compressed data."""
    
    def __init__(self, data: bytes):
        self.data = data
        self.pos = 0
        self.bit_pos = 0
    
    def read_bits(self, num_bits: int) -> Tuple[int, bool]:
        """Read num_bits bits from the stream."""
        if self.pos >= len(self.data):
            return 0, False
        
        result = 0
        bits_remaining = num_bits
        
        while bits_remaining > 0:
            if self.pos >= len(self.data):
                return 0, False
            
            bits_left_in_byte = 8 - self.bit_pos
            
            if bits_remaining >= bits_left_in_byte:
                # Read all remaining bits in current byte
                mask = (1 << bits_left_in_byte) - 1
                bits = (self.data[self.pos] >> (8 - bits_left_in_byte - self.bit_pos)) & mask
                result = (result << bits_left_in_byte) | bits
                bits_remaining -= bits_left_in_byte
                self.pos += 1
                self.bit_pos = 0
            else:
                # Read partial bits
                shift = bits_left_in_byte - bits_remaining
                mask = (1 << bits_remaining) - 1
                bits = (self.data[self.pos] >> shift) & mask
                result = (result << bits_remaining) | bits
                self.bit_pos += bits_remaining
                bits_remaining = 0
        
        return result, True


def count_leading_zeros(x: int) -> int:
    """Count leading zeros in a 64-bit integer."""
    if x == 0:
        return 64
    n = 0
    if x <= 0x00000000FFFFFFFF:
        n += 32
        x <<= 32
    if x <= 0x0000FFFFFFFFFFFF:
        n += 16
        x <<= 16
    if x <= 0x00FFFFFFFFFFFFFF:
        n += 8
        x <<= 8
    if x <= 0x0FFFFFFFFFFFFFFF:
        n += 4
        x <<= 4
    if x <= 0x3FFFFFFFFFFFFFFF:
        n += 2
        x <<= 2
    if x <= 0x7FFFFFFFFFFFFFFF:
        n += 1
    return n


def count_trailing_zeros(x: int) -> int:
    """Count trailing zeros in a 64-bit integer."""
    if x == 0:
        return 64
    n = 0
    if (x & 0x00000000FFFFFFFF) == 0:
        n += 32
        x >>= 32
    if (x & 0x000000000000FFFF) == 0:
        n += 16
        x >>= 16
    if (x & 0x00000000000000FF) == 0:
        n += 8
        x >>= 8
    if (x & 0x000000000000000F) == 0:
        n += 4
        x >>= 4
    if (x & 0x0000000000000003) == 0:
        n += 2
        x >>= 2
    if (x & 0x0000000000000001) == 0:
        n += 1
    return n


def sign_extend(value: int, bits: int) -> int:
    """Sign extend a value to 64 bits."""
    shift = 64 - bits
    return ((value << shift) >> shift) & 0xFFFFFFFFFFFFFFFF


def decompress_float64(data: bytes, count: int) -> List[float]:
    """Decompress float64 values using Gorilla compression."""
    if len(data) == 0 or count == 0:
        return []
    
    br = BitReader(data)
    result = []
    
    # Read first value (64 bits)
    first_bits, ok = br.read_bits(64)
    if not ok:
        return result
    
    first_value = struct.unpack('d', struct.pack('Q', first_bits))[0]
    result.append(first_value)
    
    if count == 1:
        return result
    
    prev_value = first_bits
    prev_leading = 0
    prev_trailing = 0
    
    while len(result) < count:
        control_bit, ok = br.read_bits(1)
        if not ok:
            break
        
        if control_bit == 0:
            # Same value as previous
            result.append(struct.unpack('d', struct.pack('Q', prev_value))[0])
        else:
            # Different value
            block_type, ok = br.read_bits(1)
            if not ok:
                break
            
            if block_type == 0:
                # Use previous leading/trailing
                meaningful_bits = 64 - prev_leading - prev_trailing
                bits, ok = br.read_bits(meaningful_bits)
                if not ok:
                    break
                xor = bits << prev_trailing
            else:
                # New leading/trailing
                leading, ok = br.read_bits(6)
                if not ok:
                    break
                meaningful_bits, ok = br.read_bits(6)
                if not ok:
                    break
                bits, ok = br.read_bits(meaningful_bits)
                if not ok:
                    break
                
                trailing = 64 - leading - meaningful_bits
                xor = bits << trailing
                
                prev_leading = leading
                prev_trailing = trailing
            
            prev_value = prev_value ^ xor
            result.append(struct.unpack('d', struct.pack('Q', prev_value))[0])
    
    return result


def decompress_int64(data: bytes, count: int) -> List[int]:
    """Decompress int64 values using Gorilla compression (delta-of-delta encoding)."""
    if len(data) == 0 or count == 0:
        return []
    
    br = BitReader(data)
    result = []
    
    # Read first value (64 bits)
    first_value, ok = br.read_bits(64)
    if not ok:
        return result
    
    result.append(sign_extend(first_value, 64))
    
    if count == 1:
        return result
    
    # Read first delta (64 bits)
    first_delta, ok = br.read_bits(64)
    if not ok:
        return result
    
    prev_value = sign_extend(first_value, 64) + sign_extend(first_delta, 64)
    result.append(prev_value)
    
    if count == 2:
        return result
    
    prev_delta = sign_extend(first_delta, 64)
    
    while len(result) < count:
        control_bit, ok = br.read_bits(1)
        if not ok:
            break
        
        if control_bit == 0:
            delta_of_delta = 0
        else:
            second_bit, ok = br.read_bits(1)
            if not ok:
                break
            
            if second_bit == 0:
                # 7 bits
                bits, ok = br.read_bits(7)
                if not ok:
                    break
                delta_of_delta = sign_extend(bits, 7)
            else:
                third_bit, ok = br.read_bits(1)
                if not ok:
                    break
                
                if third_bit == 0:
                    # 9 bits
                    bits, ok = br.read_bits(9)
                    if not ok:
                        break
                    delta_of_delta = sign_extend(bits, 9)
                else:
                    fourth_bit, ok = br.read_bits(1)
                    if not ok:
                        break
                    
                    if fourth_bit == 0:
                        # 12 bits
                        bits, ok = br.read_bits(12)
                        if not ok:
                            break
                        delta_of_delta = sign_extend(bits, 12)
                    else:
                        # 64 bits
                        bits, ok = br.read_bits(64)
                        if not ok:
                            break
                        delta_of_delta = sign_extend(bits, 64)
        
        delta = prev_delta + delta_of_delta
        value = prev_value + delta
        result.append(value)
        
        prev_value = value
        prev_delta = delta
    
    return result


def read_parq_file(filepath: str) -> List[Tuple[int, float]]:
    """Read and decompress a .parq file, returning list of (timestamp, value) tuples."""
    with open(filepath, 'rb') as f:
        data = f.read()
    
    if len(data) < HEADER_SIZE + 4:
        raise ValueError("File too small")
    
    # Read header
    magic = struct.unpack('<I', data[0:4])[0]
    if magic != MAGIC_NUMBER:
        raise ValueError(f"Invalid magic number: {hex(magic)}, expected {hex(MAGIC_NUMBER)}")
    
    format_version = struct.unpack('<I', data[4:8])[0]
    record_count = struct.unpack('<Q', data[8:16])[0]
    num_columns = struct.unpack('<I', data[16:20])[0]
    identifier = data[20:24].decode('ascii', errors='ignore')
    
    print(f"Magic: {hex(magic)}")
    print(f"Format Version: {format_version}")
    print(f"Record Count: {record_count}")
    print(f"Columns: {num_columns}")
    print(f"Identifier: {identifier}")
    print()
    
    # Read footer size (last 4 bytes)
    footer_size_offset = len(data) - 4
    footer_size = struct.unpack('<I', data[footer_size_offset:])[0]
    footer_start = footer_size_offset - footer_size
    
    if footer_start < HEADER_SIZE:
        raise ValueError("Invalid footer size")
    
    # Read footer
    footer = data[footer_start:footer_size_offset]
    metadata_version = struct.unpack('<I', footer[0:4])[0]
    footer_num_columns = struct.unpack('<I', footer[4:8])[0]
    
    if footer_num_columns != 2:
        raise ValueError(f"Expected 2 columns, got {footer_num_columns}")
    
    # Parse timestamp column metadata
    pos = 8
    timestamp_name_len = struct.unpack('<I', footer[pos:pos+4])[0]
    pos += 4
    timestamp_name = footer[pos:pos+timestamp_name_len].decode('ascii', errors='ignore')
    pos += timestamp_name_len
    timestamp_type = struct.unpack('<I', footer[pos:pos+4])[0]
    pos += 4
    timestamp_offset = struct.unpack('<Q', footer[pos:pos+8])[0]
    pos += 8
    timestamp_size = struct.unpack('<Q', footer[pos:pos+8])[0]
    pos += 8
    timestamp_count = struct.unpack('<Q', footer[pos:pos+8])[0]
    pos += 8
    
    # Parse value column metadata
    value_name_len = struct.unpack('<I', footer[pos:pos+4])[0]
    pos += 4
    value_name = footer[pos:pos+value_name_len].decode('ascii', errors='ignore')
    pos += value_name_len
    value_type = struct.unpack('<I', footer[pos:pos+4])[0]
    pos += 4
    value_offset = struct.unpack('<Q', footer[pos:pos+8])[0]
    pos += 8
    value_size = struct.unpack('<Q', footer[pos:pos+8])[0]
    pos += 8
    value_count = struct.unpack('<Q', footer[pos:pos+8])[0]
    
    print(f"Timestamp Column: {timestamp_name} (offset={timestamp_offset}, size={timestamp_size}, count={timestamp_count})")
    print(f"Value Column: {value_name} (offset={value_offset}, size={value_size}, count={value_count})")
    print()
    
    # Read and decompress timestamp column
    # Skip the 8-byte header (compression type + size)
    timestamp_data_start = timestamp_offset + 8
    timestamp_compressed = data[timestamp_data_start:timestamp_data_start + timestamp_size - 8]
    timestamps = decompress_int64(timestamp_compressed, int(timestamp_count))
    
    # Read and decompress value column
    # Skip the 8-byte header (compression type + size)
    value_data_start = value_offset + 8
    value_compressed = data[value_data_start:value_data_start + value_size - 8]
    values = decompress_float64(value_compressed, int(value_count))
    
    # Combine timestamps and values
    records = list(zip(timestamps, values))
    
    return records


def format_timestamp(ts: int) -> str:
    """Format Unix timestamp to readable date/time."""
    try:
        dt = datetime.fromtimestamp(ts)
        return dt.strftime('%Y-%m-%d %H:%M:%S')
    except (ValueError, OSError):
        return str(ts)


def main():
    if len(sys.argv) < 2:
        print("Usage: python view_parq.py <file.parq>")
        print("Example: python view_parq.py data/ing2.parq")
        sys.exit(1)
    
    filepath = sys.argv[1]
    
    try:
        records = read_parq_file(filepath)
        
        print(f"\n{'='*80}")
        print(f"Data from {filepath}")
        print(f"{'='*80}")
        print(f"{'Index':<8} {'Timestamp':<20} {'Unix Time':<15} {'Value':<15}")
        print(f"{'-'*80}")
        
        for i, (timestamp, value) in enumerate(records):
            print(f"{i+1:<8} {format_timestamp(timestamp):<20} {timestamp:<15} {value:<15.6f}")
        
        print(f"{'='*80}")
        print(f"Total records: {len(records)}")
        
    except Exception as e:
        print(f"Error reading file: {e}", file=sys.stderr)
        import traceback
        traceback.print_exc()
        sys.exit(1)


if __name__ == '__main__':
    main()


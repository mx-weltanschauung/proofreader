import React from 'react';
import { Link } from 'react-router-dom';
import type { VolumeSummary } from '../types';
import { volumeCoordinates } from '../utils/volumeCoordinates';
import { workPath } from '../utils/paths';
import './VolumeTable.css';

interface Props {
  volumes: VolumeSummary[];
}

export const VolumeTable: React.FC<Props> = ({ volumes }) => (
  <table className="volume-table">
    <thead>
      <tr>
        <th scope="col">Том</th>
        <th scope="col">Название</th>
        <th scope="col" className="volume-table-num">
          Страниц
        </th>
        <th scope="col" className="volume-table-num">
          Работ
        </th>
      </tr>
    </thead>
    <tbody>
      {volumes.map((volume) => (
        <tr key={volume.id}>
          <td className="volume-table-coord">{volumeCoordinates(volume)}</td>
          <td className="volume-table-title">
            <Link to={workPath(volume)}>{volume.title}</Link>
          </td>
          <td className="volume-table-num">{volume.pages_total}</td>
          <td className="volume-table-num">{volume.chapters_total}</td>
        </tr>
      ))}
    </tbody>
  </table>
);
